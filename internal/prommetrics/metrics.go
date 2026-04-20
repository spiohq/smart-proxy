// Package prommetrics defines Prometheus metrics for the SP-API proxy.
// All metrics use the "sp_proxy" namespace and are labeled by merchant_key,
// endpoint (canonical SP-API path), region, and where applicable method,
// status_code, and cache_status.
package prommetrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "sp_proxy"

// Metrics holds all Prometheus collectors for the proxy.
type Metrics struct {
	// --- HTTP request metrics ---

	// RequestsTotal counts proxied requests by merchant, endpoint, region, method, status code, and cache status.
	RequestsTotal *prometheus.CounterVec

	// RequestDuration observes total request duration (including queue wait) in seconds.
	RequestDuration *prometheus.HistogramVec

	// UpstreamDuration observes the upstream (Amazon) round-trip time in seconds (excludes queue wait).
	UpstreamDuration *prometheus.HistogramVec

	// RequestSizeBytes observes request body sizes.
	RequestSizeBytes *prometheus.HistogramVec

	// ResponseSizeBytes observes response body sizes.
	ResponseSizeBytes *prometheus.HistogramVec

	// --- Rate limiter metrics ---

	// RateLimitQueued counts requests that were enqueued by the rate limiter.
	RateLimitQueued *prometheus.CounterVec

	// RateLimitRejected counts requests rejected (429) by the rate limiter.
	RateLimitRejected *prometheus.CounterVec

	// RateLimitQueueDuration observes time spent waiting in the rate limit queue.
	RateLimitQueueDuration *prometheus.HistogramVec

	// RateLimitBucketsActive is the current number of active token buckets.
	RateLimitBucketsActive prometheus.Gauge

	// --- Cache metrics ---

	// CacheHitsTotal counts cache hits.
	CacheHitsTotal *prometheus.CounterVec

	// CacheMissesTotal counts cache misses.
	CacheMissesTotal *prometheus.CounterVec

	// CacheEvictionsTotal counts cache evictions.
	CacheEvictionsTotal prometheus.Counter

	// CacheSizeBytes is the current cache memory usage.
	CacheSizeBytes prometheus.Gauge

	// CacheEntries is the current number of cached entries.
	CacheEntries prometheus.Gauge

	// --- PII metrics ---

	// PIIRedactionsTotal counts responses where PII was redacted.
	PIIRedactionsTotal *prometheus.CounterVec

	// --- Upstream error metrics ---

	// UpstreamErrorsTotal counts 5xx responses from Amazon.
	UpstreamErrorsTotal *prometheus.CounterVec

	// UpstreamThrottlesTotal counts 429 responses from Amazon (distinct from proxy-generated 429s).
	UpstreamThrottlesTotal *prometheus.CounterVec
}

// New creates a Metrics instance registered with the given prometheus.Registerer.
// Pass nil to use the default global registry.
func New(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)

	durationBuckets := []float64{
		0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
	}
	sizeBuckets := prometheus.ExponentialBuckets(256, 4, 10) // 256B .. ~256KB

	m := &Metrics{
		// --- Requests ---
		RequestsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of proxied SP-API requests.",
		}, []string{"merchant_key", "endpoint", "region", "method", "status_code", "cache_status"}),

		RequestDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Total request duration including queue wait.",
			Buckets:   durationBuckets,
		}, []string{"merchant_key", "endpoint", "region", "method"}),

		UpstreamDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "upstream_duration_seconds",
			Help:      "Upstream (Amazon) round-trip time excluding queue wait.",
			Buckets:   durationBuckets,
		}, []string{"merchant_key", "endpoint", "region", "method"}),

		RequestSizeBytes: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_size_bytes",
			Help:      "Request body size in bytes.",
			Buckets:   sizeBuckets,
		}, []string{"merchant_key", "endpoint", "region", "method"}),

		ResponseSizeBytes: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "response_size_bytes",
			Help:      "Response body size in bytes.",
			Buckets:   sizeBuckets,
		}, []string{"merchant_key", "endpoint", "region", "method"}),

		// --- Rate Limiter ---
		RateLimitQueued: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ratelimit_queued_total",
			Help:      "Total requests enqueued by the rate limiter.",
		}, []string{"merchant_key", "endpoint", "region"}),

		RateLimitRejected: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ratelimit_rejected_total",
			Help:      "Total requests rejected (429) by the rate limiter.",
		}, []string{"merchant_key", "endpoint", "region"}),

		RateLimitQueueDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "ratelimit_queue_duration_seconds",
			Help:      "Time spent waiting in the rate limit queue.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60},
		}, []string{"merchant_key", "endpoint", "region"}),

		RateLimitBucketsActive: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "ratelimit_buckets_active",
			Help:      "Current number of active rate limit token buckets.",
		}),

		// --- Cache ---
		CacheHitsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hits_total",
			Help:      "Total cache hits.",
		}, []string{"merchant_key", "endpoint", "region"}),

		CacheMissesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_misses_total",
			Help:      "Total cache misses.",
		}, []string{"merchant_key", "endpoint", "region"}),

		CacheEvictionsTotal: f.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_evictions_total",
			Help:      "Total cache evictions (LRU + TTL).",
		}),

		CacheSizeBytes: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cache_size_bytes",
			Help:      "Current cache memory usage in bytes.",
		}),

		CacheEntries: f.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cache_entries",
			Help:      "Current number of cached entries.",
		}),

		// --- PII ---
		PIIRedactionsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "pii_redactions_total",
			Help:      "Total responses where PII was redacted.",
		}, []string{"merchant_key", "endpoint", "region"}),

		// --- Upstream errors ---
		UpstreamErrorsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "upstream_errors_total",
			Help:      "Total 5xx responses from Amazon SP-API.",
		}, []string{"merchant_key", "endpoint", "region"}),

		UpstreamThrottlesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "upstream_throttles_total",
			Help:      "Total 429 responses from Amazon SP-API (upstream throttling).",
		}, []string{"merchant_key", "endpoint", "region"}),
	}

	return m
}
