package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
)

type ThrottleMode string

const (
	ThrottleModeQueue        ThrottleMode = "queue"
	ThrottleModeReject       ThrottleMode = "reject"
	ThrottleModeQueueTimeout ThrottleMode = "queue-timeout"
)

func RateLimitMiddleware(limiter *Limiter, cfg *config.RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			m := merchant.MerchantFromContext(r.Context())
			endpoint := ClassifyEndpoint(r.URL.Path)
			method := r.Method
			bucket, known := limiter.GetBucket(m.Key, method, endpoint)

			if !known {
				w.Header().Set("X-SP-Proxy-Rate-Limit-Active", "false")
				next.ServeHTTP(w, r)
				return
			}

			if !waitForAppBucket(w, r, limiter, cfg, m.Key, method, endpoint) {
				return
			}

			mode := resolveThrottleMode(r, m.Key, endpoint, cfg)
			allowed, waitTime := bucket.TryConsume()
			if allowed {
				w.Header().Set("X-SP-Proxy-Queued", "false")
				w.Header().Set("X-SP-Proxy-Rate-Limit-Remaining", fmt.Sprintf("%.2f", bucket.Tokens()))
				next.ServeHTTP(w, r)
				return
			}

			serveThrottled(w, r, next, limiter, cfg, mode, bucket, waitTime, m.Key, method, endpoint)
		})
	}
}

// waitForAppBucket consumes an application-level token, optionally waiting
// for one when the configured throttle mode allows queueing. Returns false
// when the request was already failed (rejected or timed out / disconnected),
// in which case the caller must stop.
func waitForAppBucket(w http.ResponseWriter, r *http.Request, limiter *Limiter, cfg *config.RateLimitConfig, merchantKey, method, endpoint string) bool {
	appBucket, appOk := limiter.GetAppBucket(method, endpoint)
	if !appOk {
		return true
	}
	appAllowed, appWait := appBucket.TryConsume()
	if appAllowed {
		return true
	}

	mode := resolveThrottleMode(r, merchantKey, endpoint, cfg)
	if mode == ThrottleModeReject {
		w.Header().Set("Retry-After", fmt.Sprintf("%.1f", appWait.Seconds()))
		http.Error(w, "Application rate limit exceeded", http.StatusTooManyRequests)
		return false
	}

	queueStart := time.Now()
	timeout := resolveTimeout(r, cfg)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	for {
		appAllowed, appWait = appBucket.TryConsume()
		if appAllowed {
			return true
		}
		select {
		case <-ctx.Done():
			queueWaitMs := time.Since(queueStart).Milliseconds()
			w.Header().Set("X-SP-Proxy-Queued", "true")
			w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", fmt.Sprintf("%d", queueWaitMs))
			if r.Context().Err() != nil {
				w.Header().Set("X-SP-Proxy-Error-Reason", "client_disconnected_in_queue")
				w.WriteHeader(499) // Client Closed Request
				return false
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%.1f", appWait.Seconds()))
			http.Error(w, "Application rate limit queue timeout", http.StatusTooManyRequests)
			return false
		case <-time.After(appWait):
		}
	}
}

// serveThrottled handles the per-merchant bucket-empty path: reject, queue,
// or queue-with-timeout. It runs after waitForAppBucket has cleared the
// application-level limit.
func serveThrottled(w http.ResponseWriter, r *http.Request, next http.Handler, limiter *Limiter, cfg *config.RateLimitConfig, mode ThrottleMode, bucket *TokenBucket, waitTime time.Duration, merchantKey, method, endpoint string) {
	queueStart := time.Now()
	switch mode {
	case ThrottleModeReject:
		w.Header().Set("Retry-After", fmt.Sprintf("%.1f", waitTime.Seconds()))
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	case ThrottleModeQueue:
		serveQueued(w, r, next, limiter, bucket, queueStart, r.Context(), merchantKey, method, endpoint, "")
	case ThrottleModeQueueTimeout:
		timeout := resolveTimeout(r, cfg)
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		retryAfter := fmt.Sprintf("%.1f", waitTime.Seconds())
		serveQueued(w, r, next, limiter, bucket, queueStart, ctx, merchantKey, method, endpoint, retryAfter)
	}
}

// serveQueued enqueues the request and runs next on success. retryAfter is
// non-empty only for queue-timeout mode (where the caller wants Retry-After
// set on timeout responses); plain queue mode passes "".
func serveQueued(w http.ResponseWriter, r *http.Request, next http.Handler, limiter *Limiter, bucket *TokenBucket, queueStart time.Time, ctx context.Context, merchantKey, method, endpoint, retryAfter string) {
	err := limiter.EnqueueAndWait(ctx, merchantKey, method, endpoint, r)
	if err != nil {
		queueWaitMs := time.Since(queueStart).Milliseconds()
		w.Header().Set("X-SP-Proxy-Queued", "true")
		w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", fmt.Sprintf("%d", queueWaitMs))
		// Distinguish client disconnect from server-side queue timeout:
		// r.Context() canceled = client gone; ctx deadline = our timeout fired.
		if r.Context().Err() != nil || errors.Is(err, context.Canceled) {
			w.Header().Set("X-SP-Proxy-Error-Reason", "client_disconnected_in_queue")
			w.WriteHeader(499) // Client Closed Request
			return
		}
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
			http.Error(w, "Queue timeout", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "Queue full or timeout", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("X-SP-Proxy-Queued", "true")
	w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", fmt.Sprintf("%d", time.Since(queueStart).Milliseconds()))
	w.Header().Set("X-SP-Proxy-Rate-Limit-Remaining", fmt.Sprintf("%.2f", bucket.Tokens()))
	next.ServeHTTP(w, r)
}

// resolveThrottleMode resolves the throttle mode for a request. The header
// X-SP-Proxy-Throttle-Mode is advisory: it can shorten the wait or downgrade
// to "reject" (caller wants to fail fast), but it cannot upgrade to a more
// permissive mode than what the operator configured. Operator-chosen
// "reject" is treated as a ceiling that the header cannot break through.
//
// Resolution order: header > per-merchant > per-endpoint > global default.
//
// Pentest finding F-11.
func resolveThrottleMode(r *http.Request, merchantKey, endpoint string, cfg *config.RateLimitConfig) ThrottleMode {
	// Resolve the operator-chosen mode (the ceiling).
	operatorMode := ThrottleModeQueue
	if cfg.MerchantModes != nil {
		if mode, ok := cfg.MerchantModes[merchantKey]; ok {
			if m := parseThrottleMode(mode); m != "" {
				operatorMode = m
			}
		}
	}
	if cfg.EndpointModes != nil {
		if mode, ok := cfg.EndpointModes[endpoint]; ok {
			if m := parseThrottleMode(mode); m != "" {
				operatorMode = m
			}
		}
	}
	// Only fall through to default when no per-merchant or per-endpoint
	// override applies.
	if operatorMode == ThrottleModeQueue {
		if m := parseThrottleMode(cfg.DefaultMode); m != "" {
			operatorMode = m
		}
	}

	// Header is advisory. It may downgrade (queue -> reject) but not
	// upgrade (reject -> queue): the operator's "fail fast" choice is a
	// ceiling, not a floor.
	if header := r.Header.Get("X-SP-Proxy-Throttle-Mode"); header != "" {
		if requested := parseThrottleMode(header); requested != "" {
			if operatorMode == ThrottleModeReject && requested != ThrottleModeReject {
				return ThrottleModeReject
			}
			return requested
		}
	}
	return operatorMode
}

func parseThrottleMode(s string) ThrottleMode {
	switch strings.ToLower(s) {
	case "reject":
		return ThrottleModeReject
	case "queue":
		return ThrottleModeQueue
	case "queue-timeout":
		return ThrottleModeQueueTimeout
	default:
		if strings.HasPrefix(strings.ToLower(s), "queue-timeout") {
			return ThrottleModeQueueTimeout
		}
		return ""
	}
}

// resolveTimeout returns the queue-timeout duration. Like resolveThrottleMode,
// the X-SP-Proxy-Throttle-Mode header is advisory: a "queue-timeout:<N>ms"
// suffix can SHORTEN the wait but cannot LENGTHEN it beyond cfg.QueueTimeout
// (the operator's ceiling). Clients that want to fail faster can; clients
// that want to wait longer than the operator allowed cannot.
//
// Pentest finding F-11.
func resolveTimeout(r *http.Request, cfg *config.RateLimitConfig) time.Duration {
	cfgMax, _ := time.ParseDuration(cfg.QueueTimeout)
	if cfgMax <= 0 {
		cfgMax = 1 * time.Second
	}

	if header := r.Header.Get("X-SP-Proxy-Throttle-Mode"); header != "" {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			if d, err := time.ParseDuration(parts[1] + "ms"); err == nil && d > 0 {
				if d > cfgMax {
					return cfgMax
				}
				return d
			}
		}
	}
	return cfgMax
}
