package cache

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
)

type contextKey string

const requestIDContextKey contextKey = "sp-proxy-request-id"

// RequestIDFromContext retrieves the request ID stored by the logging middleware.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDContextKey).(string); ok {
		return id
	}
	return ""
}

// ContextWithRequestID stores a request ID in the context.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDContextKey, id)
}

// CacheMiddleware returns a middleware that caches GET responses by merchant+endpoint.
// It also caches POST responses for batch-cacheable endpoints (e.g. pricing batches,
// fee estimates) using order-independent body hashing.
// Cache hits skip all downstream handlers (including rate limiting).
func CacheMiddleware(c Cache, tiers *TierClassifier, cfg *config.CacheConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			m := merchant.MerchantFromContext(r.Context())
			tier := tiers.Classify(r.Method, r.URL.Path, r)

			// Non-cacheable: CacheTierNever with no special reason
			// This covers both "never-cache GETs" (feeds, notifications) and mutations (non-GET).
			// Mutations trigger prefix-based invalidation before passing through.
			if tier.Tier == CacheTierNever && tier.Reason == "" {
				if r.Method != http.MethodGet {
					InvalidateOnMutation(c, m.Key, r.Method, r.URL.Path)
				}
				next.ServeHTTP(w, r)
				return
			}

			// PII excluded (only when ExcludePII config is true)
			if cfg.ExcludePII && tier.Reason == "PII_EXCLUDED" {
				w.Header().Set("X-SP-Proxy-Cache", "PII_EXCLUDED")
				next.ServeHTTP(w, r)
				return
			}

			// Cache bypass
			if r.Header.Get("X-SP-Proxy-No-Cache") == "true" {
				w.Header().Set("X-SP-Proxy-Cache", "BYPASS")
				next.ServeHTTP(w, r)
				return
			}

			// Batch-cacheable POST: delegate to per-element batch cache logic
			if r.Method == http.MethodPost && tier.BatchCacheable {
				bcfg := lookupBatchConfig(r.URL.Path)
				if bcfg != nil {
					handleBatchCache(w, r, next, c, tier, cfg, bcfg)
					return
				}
			}

			// Single POST body-hash caching
			if r.Method == http.MethodPost && tier.PostCacheable {
				handlePostCache(w, r, next, c, tier, cfg)
				return
			}

			// Standard GET cache key. Sanitize the client-supplied custom
			// suffix (F-05): drop chars outside [A-Za-z0-9_-] and truncate
			// to 64 chars so it cannot be used as a probing oracle.
			key := GenerateCacheKey(
				m.Key, r.Method, r.URL.Path,
				r.URL.RawQuery,
				sanitizeCacheKeySuffix(r.Header.Get("X-SP-Proxy-Cache-Key")),
			)

			// Cache lookup
			cached, err := c.Get(r.Context(), key)
			if err == nil && cached != nil {
				age := time.Since(cached.CachedAt)
				remaining := cached.TTL - age
				for k, vals := range cached.Headers {
					for _, v := range vals {
						w.Header().Add(k, v)
					}
				}
				w.Header().Set("X-SP-Proxy-Cache", "HIT")
				w.Header().Set("X-SP-Proxy-Cache-Age", fmt.Sprintf("%d", int(age.Seconds())))
				w.Header().Set("X-SP-Proxy-Cache-TTL-Remaining", fmt.Sprintf("%d", int(remaining.Seconds())))
				if cached.SourceRequestID != "" {
					w.Header().Set("X-SP-Proxy-Cache-Source-ID", cached.SourceRequestID)
				}
				w.WriteHeader(cached.StatusCode)
				w.Write(cached.Body)
				return
			}

			// Cache MISS  -  record response without writing through
			rec := newResponseRecorder()
			next.ServeHTTP(rec, r)

			// Copy upstream headers to client
			for k, vals := range rec.headers {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			// Set cache status header
			w.Header().Set("X-SP-Proxy-Cache", "MISS")
			w.WriteHeader(rec.statusCode)
			w.Write(rec.body.Bytes())

			// Cache 2xx responses
			if rec.statusCode >= 200 && rec.statusCode < 300 {
				ttl := resolveTTL(r, tier, cfg)
				resp := &CachedResponse{
					StatusCode:      rec.statusCode,
					Headers:         rec.headers.Clone(),
					Body:            rec.body.Bytes(),
					CachedAt:        time.Now(),
					TTL:             ttl,
					SourceRequestID: RequestIDFromContext(r.Context()),
				}
				_ = c.Set(r.Context(), key, resp, ttl)
			}
		})
	}
}

// responseRecorder buffers the response without writing through to the underlying ResponseWriter.
type responseRecorder struct {
	headers    http.Header
	body       *bytes.Buffer
	statusCode int
}

func newResponseRecorder() *responseRecorder {
	return &responseRecorder{
		headers:    make(http.Header),
		body:       &bytes.Buffer{},
		statusCode: 200,
	}
}

func (rec *responseRecorder) Header() http.Header {
	return rec.headers
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	return rec.body.Write(b)
}

// resolveTTL determines cache TTL with priority:
// 1. X-SP-Proxy-Cache-Until (absolute RFC3339 time)
// 2. X-SP-Proxy-Cache-TTL (duration string)
// 3. Tier default TTL
//
// Client-supplied TTLs are clamped to cfg.MaxClientTTL (default 24h) to
// prevent a misbehaving caller from poisoning their merchant's cache for
// arbitrarily long durations. The tier default is NOT clamped -- operators
// who configure long tier TTLs are trusted.
//
// Pentest finding F-05.
func resolveTTL(r *http.Request, tier TierConfig, cfg *config.CacheConfig) time.Duration {
	maxTTL := 24 * time.Hour
	if cfg != nil && cfg.MaxClientTTL != "" {
		if d, err := time.ParseDuration(cfg.MaxClientTTL); err == nil && d > 0 {
			maxTTL = d
		}
	}
	clamp := func(d time.Duration) time.Duration {
		if d > maxTTL {
			return maxTTL
		}
		return d
	}

	// Priority 1: absolute time
	if until := r.Header.Get("X-SP-Proxy-Cache-Until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			if d := time.Until(t); d > 0 {
				return clamp(d)
			}
		}
	}

	// Priority 2: relative duration
	if ttlStr := r.Header.Get("X-SP-Proxy-Cache-TTL"); ttlStr != "" {
		if d, err := time.ParseDuration(ttlStr); err == nil && d > 0 {
			return clamp(d)
		}
	}

	// Priority 3: tier default (operator-trusted, not clamped)
	return tier.DefaultTTL
}

// cacheKeySuffixRe drops disallowed characters from the client-supplied
// X-SP-Proxy-Cache-Key suffix.
var cacheKeySuffixRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// sanitizeCacheKeySuffix strips disallowed characters and truncates the
// suffix to 64 chars. Without this, the client could use the suffix as an
// oracle for cache-key probing or to craft suffixes that collide with
// internal key structure.
//
// Pentest finding F-05.
func sanitizeCacheKeySuffix(s string) string {
	if s == "" {
		return ""
	}
	cleaned := cacheKeySuffixRe.ReplaceAllString(s, "")
	if len(cleaned) > 64 {
		cleaned = cleaned[:64]
	}
	return cleaned
}
