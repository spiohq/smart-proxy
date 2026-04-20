package prommetrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/spiohq/smart-proxy/internal/endpoint"
	"github.com/spiohq/smart-proxy/internal/merchant"
)

// Middleware returns an HTTP middleware that records Prometheus metrics for every
// proxied request. It should be placed as the outermost middleware so that it
// captures the full request lifecycle including queue wait time.
func Middleware(m *Metrics, region string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code and bytes written.
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Seconds()

			// Extract labels.
			merch := merchant.MerchantFromContext(r.Context())
			merchantKey := merch.Key
			ep := endpoint.Classify(r.URL.Path)
			method := r.Method
			statusCode := strconv.Itoa(rw.statusCode)
			cacheStatus := rw.Header().Get("X-SP-Proxy-Cache")
			if cacheStatus == "" {
				cacheStatus = "NONE"
			}

			// Request counter.
			m.RequestsTotal.WithLabelValues(merchantKey, ep, region, method, statusCode, cacheStatus).Inc()

			// Total duration.
			m.RequestDuration.WithLabelValues(merchantKey, ep, region, method).Observe(duration)

			// Upstream duration (exclude queue wait).
			queueWaitMs := int64(0)
			if qw := rw.Header().Get("X-SP-Proxy-Queue-Wait-Ms"); qw != "" {
				if parsed, err := strconv.ParseInt(qw, 10, 64); err == nil {
					queueWaitMs = parsed
				}
			}
			upstreamDuration := duration - float64(queueWaitMs)/1000.0
			if upstreamDuration < 0 {
				upstreamDuration = 0
			}
			// Only record upstream duration for non-cache-hits (cache hits don't go upstream).
			if cacheStatus != "HIT" {
				m.UpstreamDuration.WithLabelValues(merchantKey, ep, region, method).Observe(upstreamDuration)
			}

			// Body sizes.
			if r.ContentLength > 0 {
				m.RequestSizeBytes.WithLabelValues(merchantKey, ep, region, method).Observe(float64(r.ContentLength))
			}
			if rw.bytesWritten > 0 {
				m.ResponseSizeBytes.WithLabelValues(merchantKey, ep, region, method).Observe(float64(rw.bytesWritten))
			}

			// Rate limit queue metrics.
			if rw.Header().Get("X-SP-Proxy-Queued") == "true" {
				m.RateLimitQueued.WithLabelValues(merchantKey, ep, region).Inc()
				if queueWaitMs > 0 {
					m.RateLimitQueueDuration.WithLabelValues(merchantKey, ep, region).Observe(float64(queueWaitMs) / 1000.0)
				}
			}

			// Rate limit rejection: proxy-generated 429 (no upstream request).
			// We detect this by checking if it's a 429 AND cache status is NONE (not from upstream).
			if rw.statusCode == http.StatusTooManyRequests && cacheStatus == "NONE" {
				m.RateLimitRejected.WithLabelValues(merchantKey, ep, region).Inc()
			}

			// Cache hit/miss.
			switch cacheStatus {
			case "HIT":
				m.CacheHitsTotal.WithLabelValues(merchantKey, ep, region).Inc()
			case "MISS", "BYPASS", "PII_EXCLUDED":
				m.CacheMissesTotal.WithLabelValues(merchantKey, ep, region).Inc()
			}

			// Upstream errors (5xx from Amazon  -  only when not a cache hit).
			if cacheStatus != "HIT" && rw.statusCode >= 500 {
				m.UpstreamErrorsTotal.WithLabelValues(merchantKey, ep, region).Inc()
			}

			// Upstream throttles (429 from Amazon  -  when we did go upstream).
			if cacheStatus != "HIT" && cacheStatus != "NONE" && rw.statusCode == http.StatusTooManyRequests {
				// This 429 came from upstream, not from our rate limiter.
				m.UpstreamThrottlesTotal.WithLabelValues(merchantKey, ep, region).Inc()
			}

			// PII redaction.
			if rw.Header().Get("X-SP-Proxy-PII-Redacted") == "true" {
				m.PIIRedactionsTotal.WithLabelValues(merchantKey, ep, region).Inc()
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	wroteHeader  bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// Unwrap supports http.ResponseController and middleware that check for
// wrapped response writers.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
