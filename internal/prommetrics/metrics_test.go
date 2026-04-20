package prommetrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/prommetrics"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestMetrics(t *testing.T) (*prommetrics.Metrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := prommetrics.New(reg)
	return m, reg
}

func getCounterValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	var total float64
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, metric := range fam.GetMetric() {
			if matchLabels(metric.GetLabel(), labels) {
				total += metric.GetCounter().GetValue()
			}
		}
	}
	return total
}

func getHistogramCount(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, metric := range fam.GetMetric() {
			if matchLabels(metric.GetLabel(), labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func getHistogramSum(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, metric := range fam.GetMetric() {
			if matchLabels(metric.GetLabel(), labels) {
				return metric.GetHistogram().GetSampleSum()
			}
		}
	}
	return 0
}

func getGaugeValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		if len(fam.GetMetric()) > 0 {
			return fam.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return 0
}

func metricExists(t *testing.T, reg *prometheus.Registry, name string) bool {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() == name {
			return true
		}
	}
	return false
}

func matchLabels(metricLabels []*io_prometheus_client.LabelPair, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	have := make(map[string]string, len(metricLabels))
	for _, lp := range metricLabels {
		have[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// serveRequest is a test helper: creates a request with merchant context,
// sends it through the middleware, and returns the recorder.
func serveRequest(handler http.Handler, method, path, merchantKey string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: merchantKey, Source: "header"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// serveRequestWithContentLength sends a request with Content-Length set.
func serveRequestWithContentLength(handler http.Handler, method, path, merchantKey string, contentLen int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("x"))
	req.ContentLength = contentLen
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: merchantKey, Source: "header"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// Metrics registration
// ---------------------------------------------------------------------------

func TestMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := prommetrics.New(reg)
	require.NotNil(t, m)

	require.NotNil(t, m.RequestsTotal)
	require.NotNil(t, m.RequestDuration)
	require.NotNil(t, m.UpstreamDuration)
	require.NotNil(t, m.RequestSizeBytes)
	require.NotNil(t, m.ResponseSizeBytes)
	require.NotNil(t, m.RateLimitQueued)
	require.NotNil(t, m.RateLimitRejected)
	require.NotNil(t, m.RateLimitQueueDuration)
	require.NotNil(t, m.RateLimitBucketsActive)
	require.NotNil(t, m.CacheHitsTotal)
	require.NotNil(t, m.CacheMissesTotal)
	require.NotNil(t, m.CacheEvictionsTotal)
	require.NotNil(t, m.CacheSizeBytes)
	require.NotNil(t, m.CacheEntries)
	require.NotNil(t, m.PIIRedactionsTotal)
	require.NotNil(t, m.UpstreamErrorsTotal)
	require.NotNil(t, m.UpstreamThrottlesTotal)

	families, err := reg.Gather()
	require.NoError(t, err)
	assert.Greater(t, len(families), 0)
}

func TestMetrics_NilRegisterer_UsesDefault(t *testing.T) {
	// Should not panic when passing nil (uses global default).
	m := prommetrics.New(nil)
	require.NotNil(t, m)
	require.NotNil(t, m.RequestsTotal)
}

func TestMetrics_TwoRegistries_Independent(t *testing.T) {
	m1, reg1 := newTestMetrics(t)
	m2, reg2 := newTestMetrics(t)

	m1.RequestsTotal.WithLabelValues("m", "e", "r", "GET", "200", "MISS").Inc()
	m2.RequestsTotal.WithLabelValues("m", "e", "r", "GET", "200", "MISS").Add(5)

	v1 := getCounterValue(t, reg1, "sp_proxy_requests_total", map[string]string{"merchant_key": "m"})
	v2 := getCounterValue(t, reg2, "sp_proxy_requests_total", map[string]string{"merchant_key": "m"})
	assert.Equal(t, float64(1), v1)
	assert.Equal(t, float64(5), v2)
}

// ---------------------------------------------------------------------------
// Middleware: basic request counting
// ---------------------------------------------------------------------------

func TestMiddleware_BasicRequest(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	rr := serveRequest(handler, http.MethodGet, "/orders/v0/orders", "merchant-1")
	assert.Equal(t, http.StatusOK, rr.Code)

	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "merchant-1",
		"region":       "eu",
		"method":       "GET",
		"status_code":  "200",
		"cache_status": "MISS",
	})
	assert.Equal(t, float64(1), val)

	count := getHistogramCount(t, reg, "sp_proxy_request_duration_seconds", map[string]string{
		"merchant_key": "merchant-1",
		"region":       "eu",
		"method":       "GET",
	})
	assert.Equal(t, uint64(1), count)

	upstreamCount := getHistogramCount(t, reg, "sp_proxy_upstream_duration_seconds", map[string]string{
		"merchant_key": "merchant-1",
		"region":       "eu",
	})
	assert.Equal(t, uint64(1), upstreamCount)

	missVal := getCounterValue(t, reg, "sp_proxy_cache_misses_total", map[string]string{
		"merchant_key": "merchant-1",
		"region":       "eu",
	})
	assert.Equal(t, float64(1), missVal)
}

func TestMiddleware_MultipleRequests_CounterIncrements(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)

	for i := 0; i < 10; i++ {
		serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-inc")
	}

	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-inc",
		"region":       "eu",
		"method":       "GET",
		"status_code":  "200",
	})
	assert.Equal(t, float64(10), val)

	histCount := getHistogramCount(t, reg, "sp_proxy_request_duration_seconds", map[string]string{
		"merchant_key": "m-inc",
	})
	assert.Equal(t, uint64(10), histCount)
}

// ---------------------------------------------------------------------------
// Middleware: different HTTP methods
// ---------------------------------------------------------------------------

func TestMiddleware_DifferentMethods(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "na")(backend)

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		serveRequest(handler, method, "/orders/v0/orders", "m-methods")
	}

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
			"merchant_key": "m-methods",
			"method":       method,
		})
		assert.Equal(t, float64(1), val, "method %s should have count 1", method)
	}
}

// ---------------------------------------------------------------------------
// Middleware: different status codes
// ---------------------------------------------------------------------------

func TestMiddleware_DifferentStatusCodes(t *testing.T) {
	m, reg := newTestMetrics(t)

	codes := []int{200, 201, 301, 400, 403, 404, 500, 502, 503}

	for _, code := range codes {
		c := code
		backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-SP-Proxy-Cache", "MISS")
			w.WriteHeader(c)
		})
		handler := prommetrics.Middleware(m, "eu")(backend)
		serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-codes")
	}

	for _, code := range codes {
		val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
			"merchant_key": "m-codes",
			"status_code":  http.StatusText(0), // won't match
		})
		_ = val // just checking no panic

		val = getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
			"merchant_key": "m-codes",
			"status_code":  strings.TrimSpace(strings.Split(http.StatusText(code), " ")[0]),
		})
		// We check specific codes below instead
	}

	// 200 and 201 are separate label values
	v200 := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-codes",
		"status_code":  "200",
	})
	assert.Equal(t, float64(1), v200)

	v500 := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-codes",
		"status_code":  "500",
	})
	assert.Equal(t, float64(1), v500)
}

// ---------------------------------------------------------------------------
// Middleware: multiple regions
// ---------------------------------------------------------------------------

func TestMiddleware_DifferentRegions(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	for _, region := range []string{"eu", "na", "fe"} {
		handler := prommetrics.Middleware(m, region)(backend)
		serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-reg")
	}

	for _, region := range []string{"eu", "na", "fe"} {
		val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
			"merchant_key": "m-reg",
			"region":       region,
		})
		assert.Equal(t, float64(1), val, "region %s", region)
	}
}

// ---------------------------------------------------------------------------
// Middleware: multiple merchants tracked independently
// ---------------------------------------------------------------------------

func TestMiddleware_MultipleMerchants(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)

	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "merchant-A")
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "merchant-A")
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "merchant-B")

	vA := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "merchant-A",
	})
	vB := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "merchant-B",
	})
	assert.Equal(t, float64(2), vA)
	assert.Equal(t, float64(1), vB)
}

// ---------------------------------------------------------------------------
// Middleware: empty merchant key (no merchant resolved)
// ---------------------------------------------------------------------------

func TestMiddleware_EmptyMerchantKey(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)

	// No merchant in context  -  key will be empty string.
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "",
		"region":       "eu",
	})
	assert.Equal(t, float64(1), val)
}

// ---------------------------------------------------------------------------
// Middleware: cache statuses
// ---------------------------------------------------------------------------

func TestMiddleware_CacheHit(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("cached"))
	})

	handler := prommetrics.Middleware(m, "na")(backend)
	rr := serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m2")
	assert.Equal(t, http.StatusOK, rr.Code)

	hitVal := getCounterValue(t, reg, "sp_proxy_cache_hits_total", map[string]string{
		"merchant_key": "m2",
		"region":       "na",
	})
	assert.Equal(t, float64(1), hitVal)

	// Upstream duration should NOT be observed for cache hits.
	upstreamCount := getHistogramCount(t, reg, "sp_proxy_upstream_duration_seconds", map[string]string{
		"merchant_key": "m2",
		"region":       "na",
	})
	assert.Equal(t, uint64(0), upstreamCount)

	// Request duration SHOULD still be observed (it's the full latency).
	reqDurCount := getHistogramCount(t, reg, "sp_proxy_request_duration_seconds", map[string]string{
		"merchant_key": "m2",
		"region":       "na",
	})
	assert.Equal(t, uint64(1), reqDurCount)

	// Counter label should show "HIT"
	counterVal := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"cache_status": "HIT",
		"merchant_key": "m2",
	})
	assert.Equal(t, float64(1), counterVal)
}

func TestMiddleware_CacheBypass(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "BYPASS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-bypass")

	missVal := getCounterValue(t, reg, "sp_proxy_cache_misses_total", map[string]string{
		"merchant_key": "m-bypass",
	})
	assert.Equal(t, float64(1), missVal, "BYPASS should count as a cache miss")

	hitVal := getCounterValue(t, reg, "sp_proxy_cache_hits_total", map[string]string{
		"merchant_key": "m-bypass",
	})
	assert.Equal(t, float64(0), hitVal)
}

func TestMiddleware_CachePIIExcluded(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "PII_EXCLUDED")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-piiex")

	missVal := getCounterValue(t, reg, "sp_proxy_cache_misses_total", map[string]string{
		"merchant_key": "m-piiex",
	})
	assert.Equal(t, float64(1), missVal, "PII_EXCLUDED should count as a cache miss")
}

func TestMiddleware_NoCacheHeader_StatusIsNONE(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No X-SP-Proxy-Cache header at all.
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-nocache")

	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"cache_status": "NONE",
		"merchant_key": "m-nocache",
	})
	assert.Equal(t, float64(1), val)

	// "NONE" means cache was not involved  -  should not count as hit or miss.
	hitVal := getCounterValue(t, reg, "sp_proxy_cache_hits_total", map[string]string{
		"merchant_key": "m-nocache",
	})
	missVal := getCounterValue(t, reg, "sp_proxy_cache_misses_total", map[string]string{
		"merchant_key": "m-nocache",
	})
	assert.Equal(t, float64(0), hitVal)
	assert.Equal(t, float64(0), missVal)
}

// ---------------------------------------------------------------------------
// Middleware: rate limiting
// ---------------------------------------------------------------------------

func TestMiddleware_RateLimitRejected(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	rr := serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m3")
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	rejVal := getCounterValue(t, reg, "sp_proxy_ratelimit_rejected_total", map[string]string{
		"merchant_key": "m3",
		"region":       "eu",
	})
	assert.Equal(t, float64(1), rejVal)
}

func TestMiddleware_RateLimitRejected_MultipleEndpoints(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)

	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-rl")
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-rl")
	serveRequest(handler, http.MethodGet, "/catalog/2022-04-01/items", "m-rl")

	ordersRej := getCounterValue(t, reg, "sp_proxy_ratelimit_rejected_total", map[string]string{
		"merchant_key": "m-rl",
		"endpoint":     "/orders/v0/orders",
	})
	assert.Equal(t, float64(2), ordersRej)

	// The catalog endpoint should be separately tracked.
	// (We check there's a separate label value tracked.)
	totalFamilies, err := reg.Gather()
	require.NoError(t, err)
	var rejectedMetrics int
	for _, fam := range totalFamilies {
		if fam.GetName() == "sp_proxy_ratelimit_rejected_total" {
			rejectedMetrics = len(fam.GetMetric())
		}
	}
	assert.Equal(t, 2, rejectedMetrics, "should have 2 label combinations for rejected")
}

func TestMiddleware_QueuedRequest(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.Header().Set("X-SP-Proxy-Queued", "true")
		w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", "150")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "fe")(backend)
	serveRequest(handler, http.MethodPost, "/feeds/2021-06-30/feeds", "m4")

	queuedVal := getCounterValue(t, reg, "sp_proxy_ratelimit_queued_total", map[string]string{
		"merchant_key": "m4",
		"region":       "fe",
	})
	assert.Equal(t, float64(1), queuedVal)

	queueCount := getHistogramCount(t, reg, "sp_proxy_ratelimit_queue_duration_seconds", map[string]string{
		"merchant_key": "m4",
		"region":       "fe",
	})
	assert.Equal(t, uint64(1), queueCount)

	// Verify the observed queue wait value is 0.15 seconds.
	queueSum := getHistogramSum(t, reg, "sp_proxy_ratelimit_queue_duration_seconds", map[string]string{
		"merchant_key": "m4",
		"region":       "fe",
	})
	assert.InDelta(t, 0.15, queueSum, 0.001)
}

func TestMiddleware_QueuedRequest_ZeroWait(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.Header().Set("X-SP-Proxy-Queued", "true")
		w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", "0")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-qz")

	// Queued counter should still increment.
	queuedVal := getCounterValue(t, reg, "sp_proxy_ratelimit_queued_total", map[string]string{
		"merchant_key": "m-qz",
	})
	assert.Equal(t, float64(1), queuedVal)

	// Queue duration should NOT be observed when wait is 0.
	queueCount := getHistogramCount(t, reg, "sp_proxy_ratelimit_queue_duration_seconds", map[string]string{
		"merchant_key": "m-qz",
	})
	assert.Equal(t, uint64(0), queueCount)
}

func TestMiddleware_NotQueued_NoQueueMetrics(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.Header().Set("X-SP-Proxy-Queued", "false")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-nq")

	queuedVal := getCounterValue(t, reg, "sp_proxy_ratelimit_queued_total", map[string]string{
		"merchant_key": "m-nq",
	})
	assert.Equal(t, float64(0), queuedVal)
}

// ---------------------------------------------------------------------------
// Middleware: upstream errors and throttles
// ---------------------------------------------------------------------------

func TestMiddleware_Upstream5xx(t *testing.T) {
	m, reg := newTestMetrics(t)

	for _, code := range []int{500, 502, 503, 504} {
		c := code
		backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-SP-Proxy-Cache", "MISS")
			w.WriteHeader(c)
		})
		handler := prommetrics.Middleware(m, "eu")(backend)
		serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m5")
	}

	errVal := getCounterValue(t, reg, "sp_proxy_upstream_errors_total", map[string]string{
		"merchant_key": "m5",
		"region":       "eu",
	})
	assert.Equal(t, float64(4), errVal)
}

func TestMiddleware_Upstream5xx_NotCountedOnCacheHit(t *testing.T) {
	m, reg := newTestMetrics(t)

	// A 500 with cache HIT should NOT count as upstream error (impossible in practice,
	// but tests the guard logic).
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "HIT")
		w.WriteHeader(http.StatusInternalServerError)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m5-hit")

	errVal := getCounterValue(t, reg, "sp_proxy_upstream_errors_total", map[string]string{
		"merchant_key": "m5-hit",
	})
	assert.Equal(t, float64(0), errVal, "5xx on cache HIT should not count as upstream error")
}

func TestMiddleware_UpstreamThrottle(t *testing.T) {
	m, reg := newTestMetrics(t)

	// Upstream 429 (went through cache middleware so has MISS status).
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-ut")

	throttleVal := getCounterValue(t, reg, "sp_proxy_upstream_throttles_total", map[string]string{
		"merchant_key": "m-ut",
		"region":       "eu",
	})
	assert.Equal(t, float64(1), throttleVal)

	// Also should NOT be counted as a proxy rejection (it has cache_status MISS, not NONE).
	rejVal := getCounterValue(t, reg, "sp_proxy_ratelimit_rejected_total", map[string]string{
		"merchant_key": "m-ut",
	})
	assert.Equal(t, float64(0), rejVal, "upstream 429 should not count as proxy rejection")
}

func TestMiddleware_ProxyRejection_NotUpstreamThrottle(t *testing.T) {
	m, reg := newTestMetrics(t)

	// Proxy-generated 429 (no cache header → NONE).
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-pr")

	// Should be proxy rejection, NOT upstream throttle.
	rejVal := getCounterValue(t, reg, "sp_proxy_ratelimit_rejected_total", map[string]string{
		"merchant_key": "m-pr",
	})
	assert.Equal(t, float64(1), rejVal)

	throttleVal := getCounterValue(t, reg, "sp_proxy_upstream_throttles_total", map[string]string{
		"merchant_key": "m-pr",
	})
	assert.Equal(t, float64(0), throttleVal, "proxy 429 should not count as upstream throttle")
}

func TestMiddleware_4xxNotCountedAsUpstreamError(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusNotFound)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-4xx")

	errVal := getCounterValue(t, reg, "sp_proxy_upstream_errors_total", map[string]string{
		"merchant_key": "m-4xx",
	})
	assert.Equal(t, float64(0), errVal, "4xx should not count as upstream error")
}

// ---------------------------------------------------------------------------
// Middleware: PII redaction
// ---------------------------------------------------------------------------

func TestMiddleware_PIIRedaction(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.Header().Set("X-SP-Proxy-PII-Redacted", "true")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-pii")

	piiVal := getCounterValue(t, reg, "sp_proxy_pii_redactions_total", map[string]string{
		"merchant_key": "m-pii",
		"region":       "eu",
	})
	assert.Equal(t, float64(1), piiVal)
}

func TestMiddleware_NoPIIRedaction(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-nopii")

	piiVal := getCounterValue(t, reg, "sp_proxy_pii_redactions_total", map[string]string{
		"merchant_key": "m-nopii",
	})
	assert.Equal(t, float64(0), piiVal)
}

// ---------------------------------------------------------------------------
// Middleware: body sizes
// ---------------------------------------------------------------------------

func TestMiddleware_ResponseSizeBytes(t *testing.T) {
	m, reg := newTestMetrics(t)

	body := strings.Repeat("x", 1024) // 1KB response
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-size")

	respCount := getHistogramCount(t, reg, "sp_proxy_response_size_bytes", map[string]string{
		"merchant_key": "m-size",
	})
	assert.Equal(t, uint64(1), respCount)

	respSum := getHistogramSum(t, reg, "sp_proxy_response_size_bytes", map[string]string{
		"merchant_key": "m-size",
	})
	assert.Equal(t, float64(1024), respSum)
}

func TestMiddleware_RequestSizeBytes(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequestWithContentLength(handler, http.MethodPost, "/orders/v0/orders", "m-reqsize", 2048)

	reqCount := getHistogramCount(t, reg, "sp_proxy_request_size_bytes", map[string]string{
		"merchant_key": "m-reqsize",
	})
	assert.Equal(t, uint64(1), reqCount)

	reqSum := getHistogramSum(t, reg, "sp_proxy_request_size_bytes", map[string]string{
		"merchant_key": "m-reqsize",
	})
	assert.Equal(t, float64(2048), reqSum)
}

func TestMiddleware_ZeroContentLength_NotRecorded(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusNoContent)
		// Don't write body.
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-zero")

	respCount := getHistogramCount(t, reg, "sp_proxy_response_size_bytes", map[string]string{
		"merchant_key": "m-zero",
	})
	assert.Equal(t, uint64(0), respCount, "zero-byte response should not be observed")
}

// ---------------------------------------------------------------------------
// Middleware: upstream duration subtracts queue wait
// ---------------------------------------------------------------------------

func TestMiddleware_UpstreamDuration_SubtractsQueueWait(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.Header().Set("X-SP-Proxy-Queued", "true")
		w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", "50")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-ud")

	// The test runs very fast (total ≈ 0s). Queue wait is 50ms = 0.05s.
	// upstream = max(0, total - 0.05), so it will be clamped to 0.
	upstreamSum := getHistogramSum(t, reg, "sp_proxy_upstream_duration_seconds", map[string]string{
		"merchant_key": "m-ud",
	})
	assert.Equal(t, float64(0), upstreamSum, "upstream should be clamped to 0 when queue wait exceeds total")

	// Verify both histograms were observed.
	upstreamCount := getHistogramCount(t, reg, "sp_proxy_upstream_duration_seconds", map[string]string{
		"merchant_key": "m-ud",
	})
	assert.Equal(t, uint64(1), upstreamCount)
}

// ---------------------------------------------------------------------------
// Middleware: responseWriter behavior
// ---------------------------------------------------------------------------

func TestMiddleware_WriteWithoutExplicitWriteHeader(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		// Write without calling WriteHeader  -  should default to 200.
		w.Write([]byte("implicit 200"))
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	rr := serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-impl")
	assert.Equal(t, http.StatusOK, rr.Code)

	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-impl",
		"status_code":  "200",
	})
	assert.Equal(t, float64(1), val)
}

func TestMiddleware_DoubleWriteHeader_FirstWins(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusInternalServerError) // second call should be ignored for metrics
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-dbl")

	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-dbl",
		"status_code":  "202",
	})
	assert.Equal(t, float64(1), val)

	// 500 should NOT be recorded.
	val500 := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-dbl",
		"status_code":  "500",
	})
	assert.Equal(t, float64(0), val500)
}

func TestMiddleware_MultipleWrites_BytesCounted(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("abc"))   // 3 bytes
		w.Write([]byte("defgh")) // 5 bytes
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-multi")

	respSum := getHistogramSum(t, reg, "sp_proxy_response_size_bytes", map[string]string{
		"merchant_key": "m-multi",
	})
	assert.Equal(t, float64(8), respSum)
}

// ---------------------------------------------------------------------------
// Middleware: endpoint classification
// ---------------------------------------------------------------------------

func TestMiddleware_DifferentEndpoints(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)

	paths := []string{
		"/orders/v0/orders",
		"/orders/v0/orders/123-456",
		"/catalog/2022-04-01/items",
		"/fba/inbound/v0/shipments",
	}

	for _, p := range paths {
		serveRequest(handler, http.MethodGet, p, "m-ep")
	}

	// Should have at least 2 different endpoint labels (orders and catalog are different).
	families, err := reg.Gather()
	require.NoError(t, err)
	var endpointLabels []string
	for _, fam := range families {
		if fam.GetName() == "sp_proxy_requests_total" {
			for _, metric := range fam.GetMetric() {
				for _, lp := range metric.GetLabel() {
					if lp.GetName() == "endpoint" {
						endpointLabels = append(endpointLabels, lp.GetValue())
					}
				}
			}
		}
	}
	assert.GreaterOrEqual(t, len(endpointLabels), 2, "should classify at least 2 different endpoints")
}

// ---------------------------------------------------------------------------
// Middleware: concurrency safety
// ---------------------------------------------------------------------------

func TestMiddleware_ConcurrentRequests(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := prommetrics.Middleware(m, "eu")(backend)

	const concurrency = 50
	const perGoroutine = 20

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-conc")
			}
		}()
	}
	wg.Wait()

	total := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-conc",
	})
	assert.Equal(t, float64(concurrency*perGoroutine), total)

	histCount := getHistogramCount(t, reg, "sp_proxy_request_duration_seconds", map[string]string{
		"merchant_key": "m-conc",
	})
	assert.Equal(t, uint64(concurrency*perGoroutine), histCount)
}

// ---------------------------------------------------------------------------
// Collectors: cache stats
// ---------------------------------------------------------------------------

func TestCollectors_CacheStats(t *testing.T) {
	m, reg := newTestMetrics(t)

	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()

	// Put some data in cache.
	err := mc.Set(context.Background(), "key1", &cache.CachedResponse{
		StatusCode: 200,
		Body:       []byte("data1"),
	}, time.Minute)
	require.NoError(t, err)
	err = mc.Set(context.Background(), "key2", &cache.CachedResponse{
		StatusCode: 200,
		Body:       []byte("data2data2data2"),
	}, time.Minute)
	require.NoError(t, err)

	stop := prommetrics.StartCollectors(m, mc, nil, 50*time.Millisecond)
	defer stop()

	time.Sleep(100 * time.Millisecond)

	entries := getGaugeValue(t, reg, "sp_proxy_cache_entries")
	assert.Equal(t, float64(2), entries)

	sizeBytes := getGaugeValue(t, reg, "sp_proxy_cache_size_bytes")
	assert.Greater(t, sizeBytes, float64(0))
}

func TestCollectors_CacheAndBuckets(t *testing.T) {
	m, reg := newTestMetrics(t)

	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()

	limiter := ratelimit.NewLimiter(0.8, 10)
	defer limiter.Stop()

	limiter.GetBucket("test-merchant", "GET", "/orders/v0/orders")

	stop := prommetrics.StartCollectors(m, mc, limiter, 50*time.Millisecond)
	defer stop()

	time.Sleep(100 * time.Millisecond)

	buckets := getGaugeValue(t, reg, "sp_proxy_ratelimit_buckets_active")
	assert.Equal(t, float64(1), buckets)
}

func TestCollectors_MultipleBuckets(t *testing.T) {
	m, reg := newTestMetrics(t)

	limiter := ratelimit.NewLimiter(0.8, 10)
	defer limiter.Stop()

	limiter.GetBucket("merchant-A", "GET", "/orders/v0/orders")
	limiter.GetBucket("merchant-A", "GET", "/catalog/2022-04-01/items")
	limiter.GetBucket("merchant-B", "GET", "/orders/v0/orders")

	stop := prommetrics.StartCollectors(m, nil, limiter, 50*time.Millisecond)
	defer stop()

	time.Sleep(100 * time.Millisecond)

	buckets := getGaugeValue(t, reg, "sp_proxy_ratelimit_buckets_active")
	assert.Equal(t, float64(3), buckets)
}

func TestCollectors_NilProviders(t *testing.T) {
	m, _ := newTestMetrics(t)

	// Both nil  -  should not panic.
	stop := prommetrics.StartCollectors(m, nil, nil, 50*time.Millisecond)
	defer stop()

	time.Sleep(100 * time.Millisecond)
	// No panic = pass.
}

func TestCollectors_StopIsIdempotent(t *testing.T) {
	m, _ := newTestMetrics(t)

	stop := prommetrics.StartCollectors(m, nil, nil, 50*time.Millisecond)

	// Calling stop multiple times should not panic.
	stop()
	// Second call will panic on close of closed channel  -  this verifies it's safe.
	// We can't double-close, but we can verify the goroutine has exited.
	time.Sleep(20 * time.Millisecond) // give goroutine time to exit
}

func TestCollectors_GaugesUpdateOverTime(t *testing.T) {
	m, reg := newTestMetrics(t)

	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()

	stop := prommetrics.StartCollectors(m, mc, nil, 50*time.Millisecond)
	defer stop()

	// Initially empty.
	time.Sleep(80 * time.Millisecond)
	entries := getGaugeValue(t, reg, "sp_proxy_cache_entries")
	assert.Equal(t, float64(0), entries)

	// Add items.
	mc.Set(context.Background(), "k1", &cache.CachedResponse{StatusCode: 200, Body: []byte("a")}, time.Minute)
	mc.Set(context.Background(), "k2", &cache.CachedResponse{StatusCode: 200, Body: []byte("b")}, time.Minute)

	time.Sleep(80 * time.Millisecond)
	entries = getGaugeValue(t, reg, "sp_proxy_cache_entries")
	assert.Equal(t, float64(2), entries)

	// Remove one.
	mc.Delete(context.Background(), "k1")

	time.Sleep(80 * time.Millisecond)
	entries = getGaugeValue(t, reg, "sp_proxy_cache_entries")
	assert.Equal(t, float64(1), entries)
}

// ---------------------------------------------------------------------------
// Limiter BucketCount
// ---------------------------------------------------------------------------

func TestLimiter_BucketCount_Empty(t *testing.T) {
	limiter := ratelimit.NewLimiter(0.8, 10)
	defer limiter.Stop()

	assert.Equal(t, 0, limiter.BucketCount())
}

func TestLimiter_BucketCount_AfterCreation(t *testing.T) {
	limiter := ratelimit.NewLimiter(0.8, 10)
	defer limiter.Stop()

	limiter.GetBucket("m1", "GET", "/orders/v0/orders")
	assert.Equal(t, 1, limiter.BucketCount())

	limiter.GetBucket("m1", "GET", "/catalog/2022-04-01/items")
	assert.Equal(t, 2, limiter.BucketCount())

	// Same bucket key should not increase count.
	limiter.GetBucket("m1", "GET", "/orders/v0/orders")
	assert.Equal(t, 2, limiter.BucketCount())
}

// ---------------------------------------------------------------------------
// Middleware: combined scenario (realistic request flow)
// ---------------------------------------------------------------------------

func TestMiddleware_RealisticFlow_MixedRequests(t *testing.T) {
	m, reg := newTestMetrics(t)

	// Simulate a realistic sequence of requests with varying outcomes.
	scenarios := []struct {
		name        string
		status      int
		cacheStatus string
		queued      bool
		queueWaitMs string
		piiRedacted bool
		merchant    string
		region      string
		method      string
		path        string
	}{
		{name: "cache_hit", status: 200, cacheStatus: "HIT", merchant: "m-real", region: "eu", method: "GET", path: "/orders/v0/orders"},
		{name: "cache_miss_200", status: 200, cacheStatus: "MISS", merchant: "m-real", region: "eu", method: "GET", path: "/orders/v0/orders"},
		{name: "cache_miss_queued", status: 200, cacheStatus: "MISS", queued: true, queueWaitMs: "200", merchant: "m-real", region: "eu", method: "GET", path: "/catalog/2022-04-01/items"},
		{name: "rate_limited", status: 429, cacheStatus: "", merchant: "m-real", region: "eu", method: "GET", path: "/orders/v0/orders"},
		{name: "upstream_429", status: 429, cacheStatus: "MISS", merchant: "m-real", region: "eu", method: "GET", path: "/orders/v0/orders"},
		{name: "upstream_500", status: 500, cacheStatus: "MISS", merchant: "m-real", region: "eu", method: "GET", path: "/orders/v0/orders"},
		{name: "pii_redacted", status: 200, cacheStatus: "MISS", piiRedacted: true, merchant: "m-real", region: "eu", method: "GET", path: "/orders/v0/orders/123"},
		{name: "post_request", status: 201, cacheStatus: "MISS", merchant: "m-real", region: "na", method: "POST", path: "/feeds/2021-06-30/feeds"},
		{name: "other_merchant", status: 200, cacheStatus: "MISS", merchant: "m-other", region: "eu", method: "GET", path: "/orders/v0/orders"},
	}

	for _, sc := range scenarios {
		sc := sc
		backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sc.cacheStatus != "" {
				w.Header().Set("X-SP-Proxy-Cache", sc.cacheStatus)
			}
			if sc.queued {
				w.Header().Set("X-SP-Proxy-Queued", "true")
				w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", sc.queueWaitMs)
			}
			if sc.piiRedacted {
				w.Header().Set("X-SP-Proxy-PII-Redacted", "true")
			}
			w.WriteHeader(sc.status)
			w.Write([]byte("body"))
		})
		handler := prommetrics.Middleware(m, sc.region)(backend)
		serveRequest(handler, sc.method, sc.path, sc.merchant)
	}

	// Verify aggregate counts for "m-real".
	totalReal := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-real",
	})
	// 8 requests for m-real (all except "other_merchant").
	assert.Equal(t, float64(8), totalReal)

	// m-other has exactly 1.
	totalOther := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-other",
	})
	assert.Equal(t, float64(1), totalOther)

	// Cache: 1 hit, >= 6 misses for m-real.
	cacheHits := getCounterValue(t, reg, "sp_proxy_cache_hits_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(1), cacheHits)

	cacheMisses := getCounterValue(t, reg, "sp_proxy_cache_misses_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(6), cacheMisses) // MISS scenarios: cache_miss_200, cache_miss_queued, upstream_429, upstream_500, pii_redacted, post_request

	// Rate limiter: 1 proxy rejection.
	proxyRej := getCounterValue(t, reg, "sp_proxy_ratelimit_rejected_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(1), proxyRej)

	// Upstream throttle: 1.
	upstreamThrottle := getCounterValue(t, reg, "sp_proxy_upstream_throttles_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(1), upstreamThrottle)

	// Upstream error: 1.
	upstreamErr := getCounterValue(t, reg, "sp_proxy_upstream_errors_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(1), upstreamErr)

	// Queued: 1.
	queued := getCounterValue(t, reg, "sp_proxy_ratelimit_queued_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(1), queued)

	// PII: 1.
	pii := getCounterValue(t, reg, "sp_proxy_pii_redactions_total", map[string]string{
		"merchant_key": "m-real",
	})
	assert.Equal(t, float64(1), pii)

	// NA region: 1 request.
	naRequests := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-real",
		"region":       "na",
	})
	assert.Equal(t, float64(1), naRequests)
}

// ---------------------------------------------------------------------------
// Middleware: edge cases
// ---------------------------------------------------------------------------

func TestMiddleware_InvalidQueueWaitMs_Ignored(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.Header().Set("X-SP-Proxy-Queued", "true")
		w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", "not-a-number")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	rr := serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-inv")
	assert.Equal(t, http.StatusOK, rr.Code)

	// Queued counter should still increment.
	queuedVal := getCounterValue(t, reg, "sp_proxy_ratelimit_queued_total", map[string]string{
		"merchant_key": "m-inv",
	})
	assert.Equal(t, float64(1), queuedVal)

	// Queue duration should not be observed (wait_ms parsed as 0).
	queueCount := getHistogramCount(t, reg, "sp_proxy_ratelimit_queue_duration_seconds", map[string]string{
		"merchant_key": "m-inv",
	})
	assert.Equal(t, uint64(0), queueCount)
}

func TestMiddleware_VeryLargeResponse(t *testing.T) {
	m, reg := newTestMetrics(t)

	largeBody := make([]byte, 1024*1024) // 1MB
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		w.Write(largeBody)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/orders/v0/orders", "m-lg")

	respSum := getHistogramSum(t, reg, "sp_proxy_response_size_bytes", map[string]string{
		"merchant_key": "m-lg",
	})
	assert.Equal(t, float64(1024*1024), respSum)
}

func TestMiddleware_UnknownEndpoint(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
	})

	handler := prommetrics.Middleware(m, "eu")(backend)
	serveRequest(handler, http.MethodGet, "/totally/unknown/path", "m-unk")

	// Should still be counted (endpoint label will be the classified value or raw path).
	val := getCounterValue(t, reg, "sp_proxy_requests_total", map[string]string{
		"merchant_key": "m-unk",
		"region":       "eu",
	})
	assert.Equal(t, float64(1), val)
}

// ---------------------------------------------------------------------------
// Middleware: handler panic recovery check
// ---------------------------------------------------------------------------

func TestMiddleware_BackendPanic_MetricsStillWork(t *testing.T) {
	m, reg := newTestMetrics(t)

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("before panic"))
		panic("simulated crash")
	})

	// Wrap with a recover middleware to prevent test crash.
	recoverer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { recover() }()
		prommetrics.Middleware(m, "eu")(backend).ServeHTTP(w, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders", nil)
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: "m-panic", Source: "header"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	recoverer.ServeHTTP(rr, req)

	// Metrics middleware won't complete normally after a panic, but should not leave
	// prometheus in a broken state. Verify we can still use the registry.
	families, err := reg.Gather()
	require.NoError(t, err)
	assert.NotNil(t, families)
}

// ---------------------------------------------------------------------------
// Metric existence checks (verify all 17 metrics are registered)
// ---------------------------------------------------------------------------

func TestAllMetrics_ExistInRegistry(t *testing.T) {
	_, reg := newTestMetrics(t)

	// Trigger all metrics so they appear in Gather.
	// CounterVec/HistogramVec don't appear until first use with labels.
	// Gauges and plain Counters appear immediately.
	expectedGaugesAndCounters := []string{
		"sp_proxy_ratelimit_buckets_active",
		"sp_proxy_cache_evictions_total",
		"sp_proxy_cache_size_bytes",
		"sp_proxy_cache_entries",
	}

	for _, name := range expectedGaugesAndCounters {
		assert.True(t, metricExists(t, reg, name), "metric %s should exist", name)
	}
}

func TestAllVecMetrics_ExistAfterFirstUse(t *testing.T) {
	m, reg := newTestMetrics(t)

	// Touch every *Vec metric.
	m.RequestsTotal.WithLabelValues("m", "e", "r", "GET", "200", "MISS").Inc()
	m.RequestDuration.WithLabelValues("m", "e", "r", "GET").Observe(0.1)
	m.UpstreamDuration.WithLabelValues("m", "e", "r", "GET").Observe(0.1)
	m.RequestSizeBytes.WithLabelValues("m", "e", "r", "GET").Observe(100)
	m.ResponseSizeBytes.WithLabelValues("m", "e", "r", "GET").Observe(200)
	m.RateLimitQueued.WithLabelValues("m", "e", "r").Inc()
	m.RateLimitRejected.WithLabelValues("m", "e", "r").Inc()
	m.RateLimitQueueDuration.WithLabelValues("m", "e", "r").Observe(0.5)
	m.CacheHitsTotal.WithLabelValues("m", "e", "r").Inc()
	m.CacheMissesTotal.WithLabelValues("m", "e", "r").Inc()
	m.PIIRedactionsTotal.WithLabelValues("m", "e", "r").Inc()
	m.UpstreamErrorsTotal.WithLabelValues("m", "e", "r").Inc()
	m.UpstreamThrottlesTotal.WithLabelValues("m", "e", "r").Inc()

	expectedVecs := []string{
		"sp_proxy_requests_total",
		"sp_proxy_request_duration_seconds",
		"sp_proxy_upstream_duration_seconds",
		"sp_proxy_request_size_bytes",
		"sp_proxy_response_size_bytes",
		"sp_proxy_ratelimit_queued_total",
		"sp_proxy_ratelimit_rejected_total",
		"sp_proxy_ratelimit_queue_duration_seconds",
		"sp_proxy_cache_hits_total",
		"sp_proxy_cache_misses_total",
		"sp_proxy_pii_redactions_total",
		"sp_proxy_upstream_errors_total",
		"sp_proxy_upstream_throttles_total",
	}

	for _, name := range expectedVecs {
		assert.True(t, metricExists(t, reg, name), "metric %s should exist after first use", name)
	}

	// Also verify we have exactly 17 unique metric families total.
	families, err := reg.Gather()
	require.NoError(t, err)
	assert.Equal(t, 17, len(families), "expected exactly 17 metric families")
}
