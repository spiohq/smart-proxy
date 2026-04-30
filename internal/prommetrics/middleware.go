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
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rw, r)

			recordRequestMetrics(m, region, r, rw, time.Since(start).Seconds())
		})
	}
}

// recordRequestMetrics emits all per-request Prometheus observations after
// the downstream handler has finished. Split out from Middleware so the
// hot path stays under gocyclo's complexity threshold.
func recordRequestMetrics(m *Metrics, region string, r *http.Request, rw *responseWriter, duration float64) {
	merchantKey := merchant.MerchantFromContext(r.Context()).Key
	ep := endpoint.Classify(r.URL.Path)
	method := r.Method
	statusCode := strconv.Itoa(rw.statusCode)
	cacheStatus := rw.Header().Get("X-SP-Proxy-Cache")
	if cacheStatus == "" {
		cacheStatus = "NONE"
	}

	m.RequestsTotal.WithLabelValues(merchantKey, ep, region, method, statusCode, cacheStatus).Inc()
	m.RequestDuration.WithLabelValues(merchantKey, ep, region, method).Observe(duration)

	queueWaitMs := parseQueueWaitMs(rw.Header().Get("X-SP-Proxy-Queue-Wait-Ms"))
	upstreamDuration := duration - float64(queueWaitMs)/1000.0
	if upstreamDuration < 0 {
		upstreamDuration = 0
	}
	if cacheStatus != "HIT" {
		m.UpstreamDuration.WithLabelValues(merchantKey, ep, region, method).Observe(upstreamDuration)
	}

	if r.ContentLength > 0 {
		m.RequestSizeBytes.WithLabelValues(merchantKey, ep, region, method).Observe(float64(r.ContentLength))
	}
	if rw.bytesWritten > 0 {
		m.ResponseSizeBytes.WithLabelValues(merchantKey, ep, region, method).Observe(float64(rw.bytesWritten))
	}

	recordRateLimitMetrics(m, merchantKey, ep, region, rw, cacheStatus, queueWaitMs)
	recordCacheAndUpstreamMetrics(m, merchantKey, ep, region, rw, cacheStatus)

	if rw.Header().Get("X-SP-Proxy-PII-Redacted") == "true" {
		m.PIIRedactionsTotal.WithLabelValues(merchantKey, ep, region).Inc()
	}
}

// parseQueueWaitMs extracts the queue-wait duration written by the rate-limit
// middleware. Returns 0 when the header is absent or unparseable.
func parseQueueWaitMs(header string) int64 {
	if header == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

// recordRateLimitMetrics emits queue-depth and rejection counters. Proxy-
// generated 429s (cache status NONE) are distinguished from upstream throttles
// by inspecting the X-SP-Proxy-Cache header.
func recordRateLimitMetrics(m *Metrics, merchantKey, ep, region string, rw *responseWriter, cacheStatus string, queueWaitMs int64) {
	if rw.Header().Get("X-SP-Proxy-Queued") == "true" {
		m.RateLimitQueued.WithLabelValues(merchantKey, ep, region).Inc()
		if queueWaitMs > 0 {
			m.RateLimitQueueDuration.WithLabelValues(merchantKey, ep, region).Observe(float64(queueWaitMs) / 1000.0)
		}
	}
	if rw.statusCode == http.StatusTooManyRequests && cacheStatus == "NONE" {
		m.RateLimitRejected.WithLabelValues(merchantKey, ep, region).Inc()
	}
}

// recordCacheAndUpstreamMetrics emits cache hit/miss counters and the
// upstream error / throttle counters. Cache hits never reach upstream, so
// they are excluded from upstream-error attribution.
func recordCacheAndUpstreamMetrics(m *Metrics, merchantKey, ep, region string, rw *responseWriter, cacheStatus string) {
	switch cacheStatus {
	case "HIT":
		m.CacheHitsTotal.WithLabelValues(merchantKey, ep, region).Inc()
	case "MISS", "BYPASS", "PII_EXCLUDED":
		m.CacheMissesTotal.WithLabelValues(merchantKey, ep, region).Inc()
	}
	if cacheStatus != "HIT" && rw.statusCode >= 500 {
		m.UpstreamErrorsTotal.WithLabelValues(merchantKey, ep, region).Inc()
	}
	if cacheStatus != "HIT" && cacheStatus != "NONE" && rw.statusCode == http.StatusTooManyRequests {
		// This 429 came from upstream, not from our rate limiter.
		m.UpstreamThrottlesTotal.WithLabelValues(merchantKey, ep, region).Inc()
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
