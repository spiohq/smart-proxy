package cache_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/stretchr/testify/assert"
)

// upstream returns a handler that counts calls and returns a fixed response.
func upstream(body string, callCount *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(body))
	})
}

func testCacheMiddleware(t *testing.T) (*cache.MemoryCache, *cache.TierClassifier, *config.CacheConfig) {
	t.Helper()
	mc := cache.NewMemoryCache(1024 * 1024)
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1024 * 1024,
		DefaultTTL: "60s",
		ExcludePII: true,
	}
	return mc, tc, cfg
}

func addMerchantCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := merchant.ContextWithMerchant(r.Context(), merchant.ResolvedMerchant{Key: "test-merchant"})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestCacheMiddleware_MissAndHit(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream(`{"orders":[]}`, &callCount)))

	// First request  -  MISS
	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Second request  -  HIT
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.NotEmpty(t, w2.Header().Get("X-SP-Proxy-Cache-Age"))
	assert.NotEmpty(t, w2.Header().Get("X-SP-Proxy-Cache-TTL-Remaining"))
	assert.Equal(t, 1, callCount, "upstream should not be called on HIT")

	body, _ := io.ReadAll(w2.Body)
	assert.Equal(t, `{"orders":[]}`, string(body))
}

func TestCacheMiddleware_NoCacheBypass(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("data", &callCount)))

	// Warm cache
	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Bypass cache
	req2 := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req2.Header.Set("X-SP-Proxy-No-Cache", "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req2)
	assert.Equal(t, "BYPASS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "bypass should call upstream")
}

func TestCacheMiddleware_NonGetNotCached(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("ok", &callCount)))

	req := httptest.NewRequest("POST", "/orders/v0/orders", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 1, callCount)

	// Second POST should also call upstream
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/orders/v0/orders", nil))
	assert.Equal(t, 2, callCount)
}

func TestCacheMiddleware_NeverTierNotCached(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("token-data", &callCount)))

	// /tokens/ is CacheTierNever (security-critical, never cache)
	req := httptest.NewRequest("GET", "/tokens/2021-03-01/restrictedDataToken", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/tokens/2021-03-01/restrictedDataToken", nil))
	assert.Equal(t, 2, callCount, "Never tier should always call upstream")
}

func TestCacheMiddleware_PIIExcluded(t *testing.T) {
	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()

	piiChecker := func(r *http.Request) bool {
		return r.URL.Query().Get("dataElements") == "buyerInfo"
	}
	tc := cache.NewTierClassifier(piiChecker)
	cfg := &config.CacheConfig{Enabled: true, MaxMemory: 1024 * 1024, DefaultTTL: "60s", ExcludePII: true}

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("pii-data", &callCount)))

	req := httptest.NewRequest("GET", "/orders/v0/orders?dataElements=buyerInfo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "PII_EXCLUDED", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Second request should also hit upstream (not cached)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders?dataElements=buyerInfo", nil))
	assert.Equal(t, 2, callCount)
}

func TestCacheMiddleware_CustomTTLHeader(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("ttl-data", &callCount)))

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Cache-TTL", "100ms")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Should be cached
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders", nil))
	assert.Equal(t, 1, callCount)

	// Wait for custom TTL to expire
	time.Sleep(150 * time.Millisecond)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders", nil))
	assert.Equal(t, 2, callCount, "entry should have expired with custom TTL")
}

func TestCacheMiddleware_MutationInvalidates(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("listing-data", &callCount)))

	// Warm cache with GET
	req := httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER/SKU1", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Verify HIT
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER/SKU1", nil))
	assert.Equal(t, 1, callCount)

	// Mutation: PATCH invalidates
	patchReq := httptest.NewRequest("PATCH", "/listings/2021-08-01/items/SELLER/SKU1", nil)
	handler.ServeHTTP(httptest.NewRecorder(), patchReq)

	// GET after mutation should MISS
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER/SKU1", nil))
	assert.Equal(t, 3, callCount, "should be MISS after mutation invalidation")
}

func TestCacheMiddleware_Non2xxNotCached(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	errorHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(500)
		w.Write([]byte("error"))
	})
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(errorHandler))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders", nil))
	assert.Equal(t, 2, callCount, "error responses should not be cached")
}

func TestCacheMiddleware_DisabledConfig(t *testing.T) {
	mc, tc, _ := testCacheMiddleware(t)
	defer mc.Close()
	cfg := &config.CacheConfig{Enabled: false}

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("data", &callCount)))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders", nil))
	assert.Equal(t, 2, callCount, "cache disabled should always call upstream")
}

func TestCacheMiddleware_PIIExcluded_Registry(t *testing.T) {
	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()

	registry := pii.NewRegistry()
	tc := cache.NewTierClassifier(registry.ContainsPII)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1024 * 1024,
		DefaultTTL: "60s",
		ExcludePII: true,
	}

	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"payload":{}}`))
	})

	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream))

	// PII endpoint should get PII_EXCLUDED
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456/buyerInfo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, "PII_EXCLUDED", rec.Header().Get("X-SP-Proxy-Cache"))

	// Second request also PII_EXCLUDED (not cached)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	assert.Equal(t, "PII_EXCLUDED", rec2.Header().Get("X-SP-Proxy-Cache"))

	// Non-PII endpoint should cache normally
	req2 := httptest.NewRequest("GET", "/catalog/2022-04-01/items", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req2)
	assert.Equal(t, "MISS", rec3.Header().Get("X-SP-Proxy-Cache"))

	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req2)
	assert.Equal(t, "HIT", rec4.Header().Get("X-SP-Proxy-Cache"))
}

func TestCacheMiddleware_SourceRequestID(t *testing.T) {
	mc := cache.NewMemoryCache(1 << 20)
	defer mc.Close()
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{Enabled: true, MaxMemory: 1 << 20, DefaultTTL: "60s"}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"data":"test"}`))
	})

	mw := cache.CacheMiddleware(mc, tc, cfg)(handler)

	// First request (MISS)  -  set request ID in context
	req1 := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	ctx1 := cache.ContextWithRequestID(req1.Context(), "req-original-001")
	ctx1 = merchant.ContextWithMerchant(ctx1, merchant.ResolvedMerchant{Key: "test-merchant"})
	req1 = req1.WithContext(ctx1)

	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req1)
	assert.Equal(t, "MISS", rec1.Header().Get("X-SP-Proxy-Cache"))

	// Second request (HIT)  -  should get source ID header
	req2 := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	ctx2 := merchant.ContextWithMerchant(req2.Context(), merchant.ResolvedMerchant{Key: "test-merchant"})
	req2 = req2.WithContext(ctx2)

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)
	assert.Equal(t, "HIT", rec2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, "req-original-001", rec2.Header().Get("X-SP-Proxy-Cache-Source-ID"))
}

func TestCacheMiddleware_CacheUntilTakesPriorityOverCacheTTL(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("ttl-priority", &callCount)))

	// Set both Cache-Until (200ms) and Cache-TTL (10s). Cache-Until should win.
	until := time.Now().Add(200 * time.Millisecond).Format(time.RFC3339Nano)
	req := httptest.NewRequest("GET", "/orders/v0/orders?q=priority", nil)
	req.Header.Set("X-SP-Proxy-Cache-Until", until)
	req.Header.Set("X-SP-Proxy-Cache-TTL", "10s")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Should be cached
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders?q=priority", nil))
	assert.Equal(t, 1, callCount)

	// Wait for Cache-Until to expire (not Cache-TTL)
	time.Sleep(300 * time.Millisecond)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/orders/v0/orders?q=priority", nil))
	assert.Equal(t, 2, callCount, "Cache-Until should have expired, not Cache-TTL")
}

func TestCacheMiddleware_DifferentQueryParamsMiss(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream(`{"data":"test"}`, &callCount)))

	// Request with includedData=attributes
	req1 := httptest.NewRequest("GET", "/catalog/2022-04-01/items?identifiers=B08N5WRWNW&includedData=attributes", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	assert.Equal(t, "MISS", w1.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Same path, different includedData=images → MISS
	req2 := httptest.NewRequest("GET", "/catalog/2022-04-01/items?identifiers=B08N5WRWNW&includedData=images", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "different query params must cause cache MISS")

	// Repeat first query → HIT
	req3 := httptest.NewRequest("GET", "/catalog/2022-04-01/items?identifiers=B08N5WRWNW&includedData=attributes", nil)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "HIT", w3.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount)
}

func TestCacheMiddleware_DeleteInvalidates(t *testing.T) {
	mc, tc, cfg := testCacheMiddleware(t)
	defer mc.Close()

	callCount := 0
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream("listing-data", &callCount)))

	// Warm cache with GET
	req := httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER/SKU1", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Verify HIT
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER/SKU1", nil))
	assert.Equal(t, 1, callCount)

	// DELETE should invalidate
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/listings/2021-08-01/items/SELLER/SKU1", nil))

	// GET after DELETE should MISS
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER/SKU1", nil))
	assert.Equal(t, 3, callCount, "should be MISS after DELETE invalidation")
}

func TestCacheMiddleware_DataElementsPIIExcluded(t *testing.T) {
	mc := cache.NewMemoryCache(1024 * 1024)
	defer mc.Close()

	registry := pii.NewRegistry()
	tc := cache.NewTierClassifier(registry.ContainsPII)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1024 * 1024,
		DefaultTTL: "60s",
		ExcludePII: true,
	}

	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"payload":{}}`))
	})

	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(upstream))

	req := httptest.NewRequest("GET", "/orders/v0/orders?dataElements=buyerInfo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, "PII_EXCLUDED", rec.Header().Get("X-SP-Proxy-Cache"))
}
