package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
)

// postCacheNonIdentityFields lists JSON field names that should be stripped
// from the request body before hashing, because they are caller-supplied
// tracking IDs that do not affect the response content.
var postCacheNonIdentityFields = map[string]bool{
	"Identifier":             true, // fees: caller correlation ID
	"clientReferenceDetails": true, // shipping v2: caller tracking
}

// handlePostCache caches single-item POST requests using a body-hash cache key.
// The request body is read, non-identity fields are stripped, and the remaining
// body is hashed to produce a deterministic cache key. The URL path is included
// in the key so that /items/{asinA}/feesEstimate and /items/{asinB}/feesEstimate
// with identical bodies still produce different keys.
func handlePostCache(
	w http.ResponseWriter, r *http.Request,
	next http.Handler,
	c Cache, tier TierConfig, cfg *config.CacheConfig,
) {
	m := merchant.MerchantFromContext(r.Context())

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("post cache: failed to read body", "path", r.URL.Path, "error", err)
		next.ServeHTTP(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Generate cache key from body hash
	key, err := generatePostCacheKey(m.Key, r.URL.Path, bodyBytes)
	if err != nil {
		slog.Warn("post cache: failed to generate key", "path", r.URL.Path, "error", err)
		next.ServeHTTP(w, r)
		return
	}

	// Cache lookup
	cached, cacheErr := c.Get(r.Context(), key)
	if cacheErr == nil && cached != nil {
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

	// MISS: forward to upstream
	rec := newResponseRecorder()
	next.ServeHTTP(rec, r)

	// Copy upstream response to client
	for k, vals := range rec.headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
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
}

// generatePostCacheKey creates a deterministic cache key for a POST request.
// Format: "merchantKey:POST:path:sha256(normalized-body)"
// Non-identity fields are stripped before hashing.
//
// The hash is the FULL SHA-256 (32 bytes / 64 hex chars), not a truncation.
// Truncating to 128 bits made birthday collisions feasible at 2^64
// requests; using the full digest costs ~32 extra bytes per cache key
// and removes the discussion entirely. F-25.
func generatePostCacheKey(merchantKey, path string, body []byte) (string, error) {
	normalized, err := normalizeBody(body)
	if err != nil {
		// Fallback: hash raw body
		h := sha256.Sum256(body)
		return fmt.Sprintf("%s:POST:%s:%x", merchantKey, path, h[:]), nil
	}
	h := sha256.Sum256(normalized)
	return fmt.Sprintf("%s:POST:%s:%x", merchantKey, path, h[:]), nil
}

// normalizeBody parses the JSON body, removes non-identity fields, and
// re-serializes to canonical JSON for deterministic hashing.
func normalizeBody(body []byte) ([]byte, error) {
	// Try as object first
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err == nil {
		stripNonIdentityFields(obj)
		return json.Marshal(obj)
	}

	// Try as array (some endpoints use bare arrays)
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err == nil {
		return json.Marshal(arr)
	}

	return nil, fmt.Errorf("body is not JSON object or array")
}

// stripNonIdentityFields recursively removes non-identity fields from a JSON object.
func stripNonIdentityFields(obj map[string]json.RawMessage) {
	for key := range obj {
		if postCacheNonIdentityFields[key] {
			delete(obj, key)
			continue
		}
		// Recurse into nested objects
		var nested map[string]json.RawMessage
		if json.Unmarshal(obj[key], &nested) == nil {
			stripNonIdentityFields(nested)
			if rewritten, err := json.Marshal(nested); err == nil {
				obj[key] = rewritten
			}
		}
	}
}
