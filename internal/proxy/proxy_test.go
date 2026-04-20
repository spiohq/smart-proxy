package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/proxy"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
	mock "github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestProxy creates a proxy pointed at the mock server instead of Amazon.
// Uses NewTestProxy (plain HTTP) to avoid mutating global RegionEndpoints.
func startTestProxy(t *testing.T, mockURL string, middlewares ...proxy.Middleware) *httptest.Server {
	t.Helper()
	host := strings.TrimPrefix(mockURL, "http://")
	rp := proxy.NewTestProxy(host)
	handler := proxy.BuildChain(rp, middlewares...)
	return httptest.NewServer(handler)
}

func startTestProxyWithRateLimit(t *testing.T, mockURL string, limiter *ratelimit.Limiter, cfg *config.RateLimitConfig) *httptest.Server {
	t.Helper()
	host := strings.TrimPrefix(mockURL, "http://")
	rp := proxy.NewTestProxyWithLimiter(host, limiter)
	resolver := merchant.NewResolver(nil)
	rlMiddleware := ratelimit.RateLimitMiddleware(limiter, cfg)
	handler := proxy.BuildChain(rp, resolver.Middleware(), rlMiddleware)
	return httptest.NewServer(handler)
}

func TestProxy_ForwardsGetRequest(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	resp, err := http.Get(proxyServer.URL + "/orders/v0/orders?MarketplaceIds=A1PA6795UKMFR9")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	// Verify the mock received the request
	lastReq := mockAPI.LastRequest()
	require.NotNil(t, lastReq)
	assert.Equal(t, "GET", lastReq.Method)
	assert.Equal(t, "/orders/v0/orders", lastReq.Path)
	assert.Equal(t, "A1PA6795UKMFR9", lastReq.Query.Get("MarketplaceIds"))
}

func TestProxy_ForwardsPostWithBody(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	body := `{"sku":"TEST-123","quantity":5}`
	resp, err := http.Post(proxyServer.URL+"/feeds/2021-06-30/documents", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	lastReq := mockAPI.LastRequest()
	require.NotNil(t, lastReq)
	assert.Equal(t, "POST", lastReq.Method)
	assert.Equal(t, body, string(lastReq.Body))
}

func TestProxy_PreservesAmazonHeaders(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|my-secret-token")
	req.Header.Set("X-Amz-Date", "20260325T120000Z")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	lastReq := mockAPI.LastRequest()
	require.NotNil(t, lastReq)
	assert.Equal(t, "Atza|my-secret-token", lastReq.Header.Get("X-Amz-Access-Token"))
	assert.Equal(t, "20260325T120000Z", lastReq.Header.Get("X-Amz-Date"))
}

func TestProxy_StripsProxyHeaders(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "should-be-stripped")
	req.Header.Set("X-SP-Proxy-No-Cache", "true")
	req.Header.Set("X-Amz-Access-Token", "Atza|keep-this")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	lastReq := mockAPI.LastRequest()
	require.NotNil(t, lastReq)
	assert.Empty(t, lastReq.Header.Get("X-SP-Proxy-Merchant-Id"))
	assert.Empty(t, lastReq.Header.Get("X-SP-Proxy-No-Cache"))
	assert.Equal(t, "Atza|keep-this", lastReq.Header.Get("X-Amz-Access-Token"))
}

func TestProxy_ResponseBodyPassthrough(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	expectedBody := `{"payload":{"Orders":[{"AmazonOrderId":"123-456-789"}]}}`
	mockAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(expectedBody),
	})

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	resp, err := http.Get(proxyServer.URL + "/orders/v0/orders")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, expectedBody, string(body))
}

func TestProxy_ErrorResponsePassthrough(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	mockAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 403,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"errors":[{"code":"Unauthorized","message":"Access denied"}]}`),
	})

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	resp, err := http.Get(proxyServer.URL + "/orders/v0/orders")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 403, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "Unauthorized")
}

func TestProxy_MerchantMiddleware_SetsMerchantHeader(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	resolver := merchant.NewResolver(nil)
	proxyServer := startTestProxy(t, mockAPI.URL, resolver.Middleware())
	defer proxyServer.Close()

	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "my-merchant")
	req.Header.Set("X-Amz-Access-Token", "Atza|token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "my-merchant", resp.Header.Get("X-SP-Proxy-Merchant-Key"))
}

func TestProxy_UnknownEndpoint_StillForwards(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	proxyServer := startTestProxy(t, mockAPI.URL)
	defer proxyServer.Close()

	resp, err := http.Get(proxyServer.URL + "/new-api/2027-01-01/widgets?foo=bar")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	lastReq := mockAPI.LastRequest()
	assert.Equal(t, "/new-api/2027-01-01/widgets", lastReq.Path)
	assert.Equal(t, "bar", lastReq.Query.Get("foo"))
}

func TestProxy_AllHTTPMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			mockAPI := mock.NewMockSPAPI()
			defer mockAPI.Close()

			proxyServer := startTestProxy(t, mockAPI.URL)
			defer proxyServer.Close()

			req, _ := http.NewRequest(method, proxyServer.URL+"/test", nil)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			lastReq := mockAPI.LastRequest()
			require.NotNil(t, lastReq)
			assert.Equal(t, method, lastReq.Method)
		})
	}
}

func TestProxy_RateLimit_RejectMode(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	proxyServer := startTestProxyWithRateLimit(t, mockAPI.URL, limiter, cfg)
	defer proxyServer.Close()

	// Known endpoint: /orders/v0/orders has burst=20
	// Send 20 requests (should all pass)
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
		req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, 200, resp.StatusCode, "request %d should pass", i+1)
	}

	// 21st should be rate limited
	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 429, resp.StatusCode)
}

func TestProxy_RateLimit_UnknownEndpoint_PassesThrough(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	proxyServer := startTestProxyWithRateLimit(t, mockAPI.URL, limiter, cfg)
	defer proxyServer.Close()

	// Unknown endpoint passes through without rate limiting
	for i := 0; i < 50; i++ {
		req, _ := http.NewRequest("GET", proxyServer.URL+"/unknown/v1/widgets", nil)
		req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, 200, resp.StatusCode)
	}
}

func TestProxy_RateLimit_DynamicRateUpdate(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	// Mock returns x-amzn-RateLimit-Limit: 1000.0 (very fast)
	mockAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           {"application/json"},
			"X-Amzn-Ratelimit-Limit": {"1000.0"},
		},
		Body: []byte(`{"payload":{}}`),
	})

	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	proxyServer := startTestProxyWithRateLimit(t, mockAPI.URL, limiter, cfg)
	defer proxyServer.Close()

	// First request triggers rate update from response header
	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	// Drain the remaining burst tokens (burst=20, used 1 = 19 remaining)
	for i := 0; i < 19; i++ {
		req, _ = http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
		req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
		resp, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Now bucket is empty. Without rate update, the original rate (0.0167/sec)
	// would make the next request wait ~60 seconds  -  it would be rejected.
	// But the rate was updated to 1000.0, so refill is nearly instant.
	time.Sleep(10 * time.Millisecond) // Tiny wait for refill at 1000/sec
	req, _ = http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode, "should pass due to dynamic rate update to 1000.0")
}

func startTestProxyWithCache(t *testing.T, mockURL string, mc *cache.MemoryCache, tc *cache.TierClassifier, cacheCfg *config.CacheConfig, limiter *ratelimit.Limiter, rlCfg *config.RateLimitConfig) *httptest.Server {
	t.Helper()
	host := strings.TrimPrefix(mockURL, "http://")
	rp := proxy.NewTestProxyWithLimiter(host, limiter)
	resolver := merchant.NewResolver(nil)
	cacheMiddleware := cache.CacheMiddleware(mc, tc, cacheCfg)
	rlMiddleware := ratelimit.RateLimitMiddleware(limiter, rlCfg)
	handler := proxy.BuildChain(rp, resolver.Middleware(), cacheMiddleware, rlMiddleware)
	return httptest.NewServer(handler)
}

func TestProxy_Cache_HitSkipsRateLimit(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()
	tc := cache.NewTierClassifier(nil)
	cacheCfg := &config.CacheConfig{Enabled: true, MaxMemory: 1024 * 1024, DefaultTTL: "60s", ExcludePII: true}

	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	rlCfg := &config.RateLimitConfig{Enabled: true, DefaultMode: "reject"}

	proxyServer := startTestProxyWithCache(t, mockAPI.URL, mc, tc, cacheCfg, limiter, rlCfg)
	defer proxyServer.Close()

	// First request: MISS (consumes 1 rate limit token)
	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "MISS", resp.Header.Get("X-SP-Proxy-Cache"))

	// Now drain rate limit tokens (burst=20, used 1, drain 19 more)
	for i := 0; i < 19; i++ {
		req, _ = http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
		req.Header.Set("X-Amz-Access-Token", "Atza|token")
		req.Header.Set("X-SP-Proxy-No-Cache", "true") // bypass cache to consume rate limit
		resp, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Rate limit should now be exhausted  -  but cache HIT skips rate limiting
	req, _ = http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|token")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode, "cache HIT should skip rate limiting")
	assert.Equal(t, "HIT", resp.Header.Get("X-SP-Proxy-Cache"))
}

func TestProxy_Cache_ResponseHeaders(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	mockAPI.SetResponse("/catalog/2022-04-01/items", mock.Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"items":[]}`),
	})

	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()
	tc := cache.NewTierClassifier(nil)
	cacheCfg := &config.CacheConfig{Enabled: true, MaxMemory: 1024 * 1024, DefaultTTL: "60s", ExcludePII: true}

	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	rlCfg := &config.RateLimitConfig{Enabled: true, DefaultMode: "reject"}

	proxyServer := startTestProxyWithCache(t, mockAPI.URL, mc, tc, cacheCfg, limiter, rlCfg)
	defer proxyServer.Close()

	// Warm cache
	req, _ := http.NewRequest("GET", proxyServer.URL+"/catalog/2022-04-01/items", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|token")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// HIT  -  check headers
	req2, _ := http.NewRequest("GET", proxyServer.URL+"/catalog/2022-04-01/items", nil)
	req2.Header.Set("X-Amz-Access-Token", "Atza|token")
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()

	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))
	assert.NotEmpty(t, resp2.Header.Get("X-SP-Proxy-Cache-Age"))
	assert.NotEmpty(t, resp2.Header.Get("X-SP-Proxy-Cache-TTL-Remaining"))
}

func TestProxy_RateLimit_ResponseHeaders(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	proxyServer := startTestProxyWithRateLimit(t, mockAPI.URL, limiter, cfg)
	defer proxyServer.Close()

	req, _ := http.NewRequest("GET", proxyServer.URL+"/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|test-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "false", resp.Header.Get("X-SP-Proxy-Queued"))
	assert.NotEmpty(t, resp.Header.Get("X-SP-Proxy-Rate-Limit-Remaining"))
}
