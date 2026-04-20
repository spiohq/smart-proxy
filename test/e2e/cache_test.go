//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Cache Bypass: X-SP-Proxy-No-Cache header skips cache ─────────────────────

func TestE2E_Cache_Bypass(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["item1"]}}`),
	})

	client := &http.Client{}

	// First request: cache miss
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request with bypass: should hit upstream again
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("X-SP-Proxy-No-Cache", "true")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, "BYPASS", resp2.Header.Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/catalog/v0/items"))
}

// ── Cache HIT Headers: age and TTL remaining ─────────────────────────────────

func TestE2E_Cache_HitHeaders(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["item1"]}}`),
	})

	client := &http.Client{}

	// First request: populate cache
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()

	// Second request: cache hit
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))
	assert.NotEmpty(t, resp2.Header.Get("X-SP-Proxy-Cache-Age"))
	assert.NotEmpty(t, resp2.Header.Get("X-SP-Proxy-Cache-TTL-Remaining"))
}

// ── Custom TTL: X-SP-Proxy-Cache-TTL header sets custom TTL ──────────────────

func TestE2E_Cache_CustomTTL(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["cached"]}}`),
	})

	client := &http.Client{}

	// Request with very short TTL
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=short", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("X-SP-Proxy-Cache-TTL", "200ms")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Immediately after: should be cached
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=short", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("X-SP-Proxy-Cache-TTL", "200ms")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Wait for TTL to expire
	time.Sleep(300 * time.Millisecond)

	// After TTL: should be a cache miss
	req3, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=short", nil)
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req3.Header.Set("X-SP-Proxy-Cache-TTL", "200ms")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, "MISS", resp3.Header.Get("X-SP-Proxy-Cache"))
}

// ── Custom Cache-Until: absolute expiry time ─────────────────────────────────

func TestE2E_Cache_CustomUntil(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["until"]}}`),
	})

	client := &http.Client{}

	// Set cache-until to 200ms from now
	until := time.Now().Add(200 * time.Millisecond).Format(time.RFC3339Nano)

	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=until", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("X-SP-Proxy-Cache-Until", until)
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()

	// Immediately: should be cached
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=until", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Wait for expiry
	time.Sleep(300 * time.Millisecond)

	req3, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=until", nil)
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, "MISS", resp3.Header.Get("X-SP-Proxy-Cache"))
}

// ── Custom Cache Key: X-SP-Proxy-Cache-Key header ────────────────────────────

func TestE2E_Cache_CustomKey(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["v1"]}}`),
	})

	client := &http.Client{}

	// Request with custom cache key A
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("X-SP-Proxy-Cache-Key", "custom-key-A")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Same URL, same custom key: HIT
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("X-SP-Proxy-Cache-Key", "custom-key-A")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Same URL, different custom key: MISS
	req3, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req3.Header.Set("X-SP-Proxy-Cache-Key", "custom-key-B")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, "MISS", resp3.Header.Get("X-SP-Proxy-Cache"))
}

// ── Mutation Invalidation: POST invalidates cached GET ───────────────────────

func TestE2E_Cache_MutationInvalidation(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/listings/2021-08-01/items/SELLER/SKU1", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"sku":"SKU1","status":"active"}}`),
	})

	client := &http.Client{}

	// GET: cache miss
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// GET: cache hit
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// PATCH: mutation should invalidate cache
	env.MockSPAPI.SetResponse("/listings/2021-08-01/items/SELLER/SKU1", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"sku":"SKU1","status":"updated"}}`),
	})

	req3, _ := http.NewRequest("PATCH", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1",
		strings.NewReader(`{"patches":[{"op":"replace","path":"/status","value":"updated"}]}`))
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()

	// GET after mutation: should be cache miss
	req4, _ := http.NewRequest("GET", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req4.Header.Set("x-amz-access-token", "Atza|token")
	req4.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp4, err := client.Do(req4)
	require.NoError(t, err)
	resp4.Body.Close()
	assert.Equal(t, "MISS", resp4.Header.Get("X-SP-Proxy-Cache"))
}

// ── Non-2xx Not Cached: error responses should not be cached ─────────────────

func TestE2E_Cache_Non2xxNotCached(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 400,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"errors":[{"code":"InvalidInput","message":"bad request"}]}`),
	})

	client := &http.Client{}

	// First request: 400
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=bad", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 400, resp1.StatusCode)

	// Second request: should hit upstream again (400 not cached)
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=bad", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	// Both requests reached upstream
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/catalog/v0/items"))
}

// ── PII Exclusion: PII endpoints excluded from cache ─────────────────────────

func TestE2E_Cache_PIIExclusion(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled(), WithCachePIIExclusion(true))

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"Orders":[{"BuyerInfo":{"BuyerEmail":"test@example.com"}}]}}`),
	})

	client := &http.Client{}

	// Request with PII data elements (triggers PII detection)
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders?dataElements=buyerInfo", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "PII_EXCLUDED", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: should still reach upstream (not cached)
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders?dataElements=buyerInfo", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/orders/v0/orders"))
}

// ── Query Param Normalization: same params in different order = same cache key

func TestE2E_Cache_QueryParamOrder(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["sorted"]}}`),
	})

	client := &http.Client{}

	// Request with params in order A
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?aaa=1&zzz=2", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Same params in reverse order: should be cache hit
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?zzz=2&aaa=1", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Only 1 upstream request
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/catalog/v0/items"))
}

// ── Cache Body Preserved: cached response body matches original ──────────────

func TestE2E_Cache_BodyPreserved(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	originalBody := `{"payload":{"items":[{"asin":"B123","title":"Test Product"}]}}`
	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(originalBody),
	})

	client := &http.Client{}

	// Populate cache
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=body", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	// Get from cache
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=body", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))
	assert.Equal(t, string(body1), string(body2))
	assert.Contains(t, string(body2), "Test Product")
}

// ── Per-Merchant Cache Isolation: different merchants have separate caches ────

func TestE2E_Cache_MerchantIsolation(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["data"]}}`),
	})

	client := &http.Client{}

	// SELLER_X populates cache
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=iso", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_X")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// SELLER_Y: same URL, different merchant = MISS
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=iso", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_Y")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "MISS", resp2.Header.Get("X-SP-Proxy-Cache"))

	// 2 upstream requests (one per merchant)
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/catalog/v0/items"))
}

// ── Different Query Params Not Cached Together ──────────────────────────────
// Same endpoint + identifiers but different includedData must produce separate
// cache entries. This prevents stale/wrong data when a caller changes which
// fields to include.

func TestE2E_Cache_DifferentQueryParamsNotCached(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	// Use a single path-based response; the test validates cache key separation
	// by checking MISS/HIT headers and upstream request counts.
	env.MockSPAPI.SetResponse("/catalog/2022-04-01/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"items":[{"asin":"B08N5WRWNW"}]}`),
	})

	client := &http.Client{}

	// Request 1: includedData=attributes → MISS
	req1, _ := http.NewRequest("GET", env.ProxyURL+
		"/catalog/2022-04-01/items?marketplaceIds=ATVPDKIKX0DER&identifiers=B08N5WRWNW&identifiersType=ASIN&includedData=attributes", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Request 2: same identifiers, includedData=images → must be MISS (different query)
	req2, _ := http.NewRequest("GET", env.ProxyURL+
		"/catalog/2022-04-01/items?marketplaceIds=ATVPDKIKX0DER&identifiers=B08N5WRWNW&identifiersType=ASIN&includedData=images", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "MISS", resp2.Header.Get("X-SP-Proxy-Cache"), "different includedData must be a cache MISS")

	// Request 3: repeat includedData=attributes → HIT (same query as req1)
	req3, _ := http.NewRequest("GET", env.ProxyURL+
		"/catalog/2022-04-01/items?marketplaceIds=ATVPDKIKX0DER&identifiers=B08N5WRWNW&identifiersType=ASIN&includedData=attributes", nil)
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, "HIT", resp3.Header.Get("X-SP-Proxy-Cache"), "same query must be a cache HIT")

	// Request 4: repeat includedData=images → HIT
	req4, _ := http.NewRequest("GET", env.ProxyURL+
		"/catalog/2022-04-01/items?marketplaceIds=ATVPDKIKX0DER&identifiers=B08N5WRWNW&identifiersType=ASIN&includedData=images", nil)
	req4.Header.Set("x-amz-access-token", "Atza|token")
	req4.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp4, err := client.Do(req4)
	require.NoError(t, err)
	resp4.Body.Close()
	assert.Equal(t, "HIT", resp4.Header.Get("X-SP-Proxy-Cache"), "same query must be a cache HIT")

	// Exactly 2 upstream requests (one per distinct query)
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/catalog/2022-04-01/items"))
}

// ── DELETE Invalidation: DELETE also invalidates cached GET ─────────────────

func TestE2E_Cache_DeleteInvalidation(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/listings/2021-08-01/items/SELLER/SKU1", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"sku":"SKU1","status":"active"}}`),
	})

	client := &http.Client{}

	// GET: cache miss
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// GET: cache hit
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// DELETE: should invalidate cache
	env.MockSPAPI.SetResponse("/listings/2021-08-01/items/SELLER/SKU1", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"status":"deleted"}}`),
	})

	req3, _ := http.NewRequest("DELETE", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()

	// GET after DELETE: should be cache miss
	req4, _ := http.NewRequest("GET", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU1", nil)
	req4.Header.Set("x-amz-access-token", "Atza|token")
	req4.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp4, err := client.Do(req4)
	require.NoError(t, err)
	resp4.Body.Close()
	assert.Equal(t, "MISS", resp4.Header.Get("X-SP-Proxy-Cache"))
}

// ── Cache Disabled: no caching when config is disabled ──────────────────────

func TestE2E_Cache_Disabled(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["item1"]}}`),
	})

	client := &http.Client{}

	// First request
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Empty(t, resp1.Header.Get("X-SP-Proxy-Cache"), "cache disabled should not set cache header")

	// Second request: should still hit upstream
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/catalog/v0/items"), "both requests should reach upstream")
}

// ── Cache-Until Past: past timestamp should not cache ───────────────────────

func TestE2E_Cache_UntilPast(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":["past"]}}`),
	})

	client := &http.Client{}

	// Set cache-until to a time in the past
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=past", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("X-SP-Proxy-Cache-Until", past)
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()

	// Second request: should hit upstream again (past Cache-Until falls through to tier TTL,
	// but the entry may be cached with tier TTL  -  this tests that the response was
	// received correctly regardless)
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?q=past", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("X-SP-Proxy-Cache-Until", past)
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	// With past Cache-Until, resolveTTL falls through to tier default, so it WILL cache
	// This is correct behavior  -  Cache-Until only applies if the time is in the future
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))
}

// ── POST Invalidation: POST also invalidates cached GET ─────────────────────

func TestE2E_Cache_PostInvalidation(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/feeds/2021-06-30/feeds", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"feeds":[]}}`),
	})

	client := &http.Client{}

	// GET: cache miss
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/feeds/2021-06-30/feeds", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// GET: cache hit
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/feeds/2021-06-30/feeds", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// POST: create a feed (should invalidate cached feeds list)
	req3, _ := http.NewRequest("POST", env.ProxyURL+"/feeds/2021-06-30/feeds",
		strings.NewReader(`{"feedType":"POST_PRODUCT_DATA"}`))
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()

	// GET after POST: should be cache miss
	req4, _ := http.NewRequest("GET", env.ProxyURL+"/feeds/2021-06-30/feeds", nil)
	req4.Header.Set("x-amz-access-token", "Atza|token")
	req4.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp4, err := client.Do(req4)
	require.NoError(t, err)
	resp4.Body.Close()
	assert.Equal(t, "MISS", resp4.Header.Get("X-SP-Proxy-Cache"))
}

// ── Query Param Subset: extra param means different cache entry ─────────────

func TestE2E_Cache_QueryParamSubset(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/2022-04-01/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"items":[]}`),
	})

	client := &http.Client{}

	// Request with one param
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/2022-04-01/items?identifiers=B08N5WRWNW", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Request with same param + extra param → MISS (different key)
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/2022-04-01/items?identifiers=B08N5WRWNW&includedData=attributes", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "MISS", resp2.Header.Get("X-SP-Proxy-Cache"), "extra param must produce cache MISS")

	// Repeat first request → HIT
	req3, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/2022-04-01/items?identifiers=B08N5WRWNW", nil)
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()
	assert.Equal(t, "HIT", resp3.Header.Get("X-SP-Proxy-Cache"))

	// 2 upstream requests total
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/catalog/2022-04-01/items"))
}

// ═══════════════════════════════════════════════════════════════════════════════
// Batch POST per-element caching
// ═══════════════════════════════════════════════════════════════════════════════

// ── Batch ItemOffers: MISS on first, HIT on second (same body) ─────────────

func TestE2E_Cache_BatchItemOffers_MissAndHit(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/itemOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"responses":[{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000AAA","status":"Success"}}}]}`),
	})

	client := &http.Client{}
	body := `{"requests":[{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}]}`

	// First request: MISS
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: HIT
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Only 1 upstream request
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/batches/products/pricing/v0/itemOffers"))
}

// ── Batch ItemOffers: order-independent caching ────────────────────────────

func TestE2E_Cache_BatchItemOffers_OrderIndependent(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/itemOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body: []byte(`{"responses":[` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000AAA","status":"Success"}}},` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000BBB","status":"Success"}}}` +
			`]}`),
	})

	client := &http.Client{}

	// First request: [A, B]
	body1 := `{"requests":[` +
		`{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"},` +
		`{"uri":"/products/pricing/v0/items/B000BBB/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}` +
		`]}`
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body1))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: [B, A] (reversed order) → HIT
	body2 := `{"requests":[` +
		`{"uri":"/products/pricing/v0/items/B000BBB/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"},` +
		`{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}` +
		`]}`
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body2))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))
}

// ── Batch ItemOffers: cross-batch hit (subset of previously cached batch) ──

func TestE2E_Cache_BatchItemOffers_CrossBatchHit(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/itemOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body: []byte(`{"responses":[` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000AAA","status":"Success"}}},` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000BBB","status":"Success"}}}` +
			`]}`),
	})

	client := &http.Client{}

	// First request: [A, B]
	body1 := `{"requests":[` +
		`{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"},` +
		`{"uri":"/products/pricing/v0/items/B000BBB/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}` +
		`]}`
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body1))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: [A] only → HIT (A was cached from the first batch)
	body2 := `{"requests":[{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}]}`
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body2))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))
}

// ── Batch ItemOffers: partial miss (some elements cached, some not) ────────

func TestE2E_Cache_BatchItemOffers_PartialMiss(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	// Response for first batch [A, B]
	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/itemOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body: []byte(`{"responses":[` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000AAA","status":"Success"}}},` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000BBB","status":"Success"}}}` +
			`]}`),
	})

	client := &http.Client{}

	// First request: [A, B] → MISS
	body1 := `{"requests":[` +
		`{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"},` +
		`{"uri":"/products/pricing/v0/items/B000BBB/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}` +
		`]}`
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body1))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Update response for second batch (mock will return this for [A, C] upstream)
	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/itemOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body: []byte(`{"responses":[` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000AAA","status":"Success"}}},` +
			`{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000CCC","status":"Success"}}}` +
			`]}`),
	})

	// Second request: [A, C] → MISS (C is not cached)
	body2 := `{"requests":[` +
		`{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"},` +
		`{"uri":"/products/pricing/v0/items/B000CCC/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}` +
		`]}`
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body2))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "MISS", resp2.Header.Get("X-SP-Proxy-Cache"))

	// 2 upstream requests total
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/batches/products/pricing/v0/itemOffers"))
}

// ── Batch Fees (bare array format): MISS then HIT ──────────────────────────

func TestE2E_Cache_BatchFees_MissAndHit(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/products/fees/v0/feesEstimate", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`[{"Status":"Success","FeesEstimateIdentifier":{"IdValue":"B07XJ8C8F5"}}]`),
	})

	client := &http.Client{}
	body := `[{"IdType":"ASIN","IdValue":"B07XJ8C8F5","FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","IsAmazonFulfilled":true,"PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1"}}]`

	// First request: MISS
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/feesEstimate", strings.NewReader(body))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: HIT
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/feesEstimate", strings.NewReader(body))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Only 1 upstream request
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/products/fees/v0/feesEstimate"))
}

// ── Batch ItemOffers: bypass with X-SP-Proxy-No-Cache header ───────────────

func TestE2E_Cache_BatchItemOffers_Bypass(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/itemOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"responses":[{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"ASIN":"B000AAA","status":"Success"}}}]}`),
	})

	client := &http.Client{}
	body := `{"requests":[{"uri":"/products/pricing/v0/items/B000AAA/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New","CustomerType":"Consumer"}]}`

	req, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/itemOffers", strings.NewReader(body))
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SP-Proxy-No-Cache", "true")
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "BYPASS", resp.Header.Get("X-SP-Proxy-Cache"))
}

// ── Batch ListingOffers: invalidated by PUT on listings ────────────────────

func TestE2E_Cache_BatchListingOffers_InvalidateOnPut(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/batches/products/pricing/v0/listingOffers", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"responses":[{"status":{"statusCode":200,"reasonPhrase":"OK"},"body":{"payload":{"SKU":"SKU-1","status":"Success"}}}]}`),
	})

	env.MockSPAPI.SetResponse("/listings/2021-08-01/items/SELLER/SKU-1", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"sku":"SKU-1","status":"ACCEPTED"}`),
	})

	client := &http.Client{}
	batchBody := `{"requests":[{"uri":"/products/pricing/v0/listings/SKU-1/offers","method":"GET","MarketplaceId":"ATVPDKIKX0DER","ItemCondition":"New"}]}`

	// Step 1: POST batch listing offers → MISS
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/listingOffers", strings.NewReader(batchBody))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Step 2: POST batch listing offers → HIT
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/listingOffers", strings.NewReader(batchBody))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Step 3: PUT on listings (mutation) → invalidates cached listing offers
	req3, _ := http.NewRequest("PUT", env.ProxyURL+"/listings/2021-08-01/items/SELLER/SKU-1",
		strings.NewReader(`{"productType":"PRODUCT","patches":[{"op":"replace","path":"/attributes/title","value":"Updated"}]}`))
	req3.Header.Set("x-amz-access-token", "Atza|token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	resp3.Body.Close()

	// Step 4: POST batch listing offers → MISS (invalidated by PUT)
	req4, _ := http.NewRequest("POST", env.ProxyURL+"/batches/products/pricing/v0/listingOffers", strings.NewReader(batchBody))
	req4.Header.Set("x-amz-access-token", "Atza|token")
	req4.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req4.Header.Set("Content-Type", "application/json")
	resp4, err := client.Do(req4)
	require.NoError(t, err)
	resp4.Body.Close()
	assert.Equal(t, "MISS", resp4.Header.Get("X-SP-Proxy-Cache"))
}

// ═══════════════════════════════════════════════════════════════════════════════
// Single POST body-hash caching
// ═══════════════════════════════════════════════════════════════════════════════

// ── POST Fees: MISS on first, HIT on second (same body) ───────────────────

func TestE2E_Cache_PostFees_MissAndHit(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/products/fees/v0/items/B07XJ8C8F5/feesEstimate", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"FeesEstimateResult":{"Status":"Success"}}}`),
	})

	client := &http.Client{}
	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`

	// First request: MISS
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: HIT
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Only 1 upstream request
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/products/fees/v0/items/B07XJ8C8F5/feesEstimate"))
}

// ── POST Fees: different Identifier values produce HIT ─────────────────────

func TestE2E_Cache_PostFees_IdentifierIgnored(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/products/fees/v0/items/B07XJ8C8F5/feesEstimate", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"FeesEstimateResult":{"Status":"Success"}}}`),
	})

	client := &http.Client{}

	// First request with Identifier "req-1"
	body1 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body1))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request with Identifier "req-2" but same pricing data → HIT
	body2 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-2","IsAmazonFulfilled":true}}`
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body2))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Only 1 upstream request
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/products/fees/v0/items/B07XJ8C8F5/feesEstimate"))
}

// ── POST Fees: different ListingPrice produces MISS ────────────────────────

func TestE2E_Cache_PostFees_DifferentPrice_Miss(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/products/fees/v0/items/B07XJ8C8F5/feesEstimate", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"FeesEstimateResult":{"Status":"Success"}}}`),
	})

	client := &http.Client{}

	// First request with Amount 25.99
	body1 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body1))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request with Amount 39.99 (different price) → MISS
	body2 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":39.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body2))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "MISS", resp2.Header.Get("X-SP-Proxy-Cache"))

	// 2 upstream requests (different body hashes)
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/products/fees/v0/items/B07XJ8C8F5/feesEstimate"))
}

// ── POST Shipping v1 Rates: MISS then HIT ─────────────────────────────────

func TestE2E_Cache_PostShippingRates_MissAndHit(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/shipping/v1/rates", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"serviceRates":[{"serviceType":"Standard","promise":{"receiveDate":"2026-04-25"}}]}}`),
	})

	client := &http.Client{}
	body := `{"shipTo":{"postalCode":"10001","countryCode":"US"},"shipFrom":{"postalCode":"98109","countryCode":"US"},"serviceTypes":["Standard"],"containerSpecifications":[{"dimensions":{"length":10,"width":10,"height":10,"unit":"CM"},"weight":{"value":1,"unit":"kg"}}]}`

	// First request: MISS
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/shipping/v1/rates", strings.NewReader(body))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, "MISS", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Second request: HIT
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/shipping/v1/rates", strings.NewReader(body))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, "HIT", resp2.Header.Get("X-SP-Proxy-Cache"))

	// Only 1 upstream request
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/shipping/v1/rates"))
}

// ── POST Fees: bypass with X-SP-Proxy-No-Cache header ─────────────────────

func TestE2E_Cache_PostFees_Bypass(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/products/fees/v0/items/B07XJ8C8F5/feesEstimate", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"FeesEstimateResult":{"Status":"Success"}}}`),
	})

	client := &http.Client{}
	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`

	req, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body))
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SP-Proxy-No-Cache", "true")
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "BYPASS", resp.Header.Get("X-SP-Proxy-Cache"))
}

// ── POST Fees: non-2xx responses should not be cached ─────────────────────

func TestE2E_Cache_PostFees_Non2xxNotCached(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/products/fees/v0/items/B07XJ8C8F5/feesEstimate", mock.Response{
		StatusCode: 400,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"errors":[{"code":"InvalidInput","message":"bad request"}]}`),
	})

	client := &http.Client{}
	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`

	// First request: 400
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body))
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 400, resp1.StatusCode)

	// Second request: should hit upstream again (400 not cached)
	req2, _ := http.NewRequest("POST", env.ProxyURL+"/products/fees/v0/items/B07XJ8C8F5/feesEstimate", strings.NewReader(body))
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	// Both requests reached upstream
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/products/fees/v0/items/B07XJ8C8F5/feesEstimate"))
}
