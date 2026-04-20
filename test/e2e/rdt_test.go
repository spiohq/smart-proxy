//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rdtTokenResponse returns a mock Tokens API response body.
func rdtTokenResponse(token string) []byte {
	b, _ := json.Marshal(map[string]any{
		"restrictedDataToken": token,
		"expiresIn":           3600,
	})
	return b
}

func jsonHeaders() http.Header {
	return http.Header{
		"Content-Type":            []string{"application/json"},
		"x-amzn-RateLimit-Limit": []string{"1.0"},
	}
}

// ── Auto-Mint: cache miss triggers upstream RDT mint ───────────────────────

func TestE2E_RDT_AutoMint_OrdersEndpoint(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	// Mock the Tokens API endpoint
	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|e2e-rdt-orders"),
	})

	// Mock the actual Orders endpoint
	env.MockSPAPI.SetResponse("/orders/v0/orders/123-456", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123-456","OrderStatus":"Shipped"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), "123-456")

	// Verify: Tokens API was called once
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))

	// Verify: the order endpoint was called with the RDT, not the LWA token
	last := env.MockSPAPI.LastRequest()
	require.NotNil(t, last)
	assert.Equal(t, "/orders/v0/orders/123-456", last.Path)
	assert.Equal(t, "Atz.sprdt|e2e-rdt-orders", last.Header.Get("X-Amz-Access-Token"))
}

// ── RDT Cache Hit: second request reuses cached RDT ────────────────────────

func TestE2E_RDT_CacheHit_NoSecondMint(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|e2e-cached-rdt"),
	})

	env.MockSPAPI.SetResponse("/orders/v0/orders/AAA", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"AAA"}}`),
	})
	env.MockSPAPI.SetResponse("/orders/v0/orders/BBB", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"BBB"}}`),
	})

	client := &http.Client{}

	// First request: cache miss, mints RDT
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/AAA", nil)
	req1.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 200, resp1.StatusCode)

	// Second request: different order, same operation -> cache hit
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/BBB", nil)
	req2.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode)

	// Only 1 Tokens API call
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Already RDT: passthrough without minting ───────────────────────────────

func TestE2E_RDT_AlreadyRDT_Passthrough(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atz.sprdt|client-already-has-rdt")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	// No Tokens API call
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
	// Upstream received the original RDT
	last := env.MockSPAPI.LastRequest()
	assert.Equal(t, "Atz.sprdt|client-already-has-rdt", last.Header.Get("X-Amz-Access-Token"))
}

// ── Feature Off: no minting at all ─────────────────────────────────────────

func TestE2E_RDT_FeatureOff_Passthrough(t *testing.T) {
	// No WithRDTAutoMint() -> feature is off
	env := NewTestEnv(t, WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	// No Tokens API call
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
	// Upstream received the original LWA token
	last := env.MockSPAPI.LastRequest()
	assert.Equal(t, "Atza|my-lwa-token", last.Header.Get("X-Amz-Access-Token"))
}

// ── Non-PII Endpoint: no minting ───────────────────────────────────────────

func TestE2E_RDT_NonPIIEndpoint_NoMint(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/catalog/2022-04-01/items/B07XYZ", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"asin":"B07XYZ"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/2022-04-01/items/B07XYZ", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Mint Failure: fail open with original token ────────────────────────────

func TestE2E_RDT_MintFailure_FailOpen(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	// Tokens API returns 500
	env.MockSPAPI.SetError("/tokens/2021-03-01/restrictedDataToken", 500)

	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	// Request should succeed (fail-open: original token forwarded)
	assert.Equal(t, 200, resp.StatusCode)
	// Upstream received the original LWA token (not an RDT)
	last := env.MockSPAPI.LastRequest()
	assert.Equal(t, "Atza|lwa-token", last.Header.Get("X-Amz-Access-Token"))
}

// ── 403 After Swap: cache invalidation ─────────────────────────────────────

func TestE2E_RDT_403_InvalidatesCache(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|revoked-rdt"),
	})

	// First: orders endpoint returns 403 (simulating revoked RDT)
	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 403,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"errors":[{"code":"AccessDenied"}]}`),
	})

	req1, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req1.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, err := http.DefaultClient.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 403, resp1.StatusCode)

	mintCountAfterFirst := env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken")
	assert.Equal(t, 1, mintCountAfterFirst)

	// Now fix: orders endpoint returns 200, tokens API returns new RDT
	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|fresh-rdt"),
	})
	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123"}}`),
	})

	// Second request should trigger a new mint (cache was invalidated)
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req2.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode)

	// Should have minted again (2 total)
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Force-RDT: true forces minting on any endpoint ─────────────────────────

func TestE2E_RDT_ForceHeader_True(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|forced-rdt"),
	})

	env.MockSPAPI.SetResponse("/reports/2021-06-30/documents/doc-from-notification", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportDocumentId":"doc-from-notification","url":"https://s3.amazonaws.com/..."}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/reports/2021-06-30/documents/doc-from-notification", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))

	// Verify the force header was stripped before reaching upstream
	last := env.MockSPAPI.LastRequest()
	assert.Empty(t, last.Header.Get("X-SP-Proxy-Force-RDT"))
	assert.Equal(t, "Atz.sprdt|forced-rdt", last.Header.Get("X-Amz-Access-Token"))
}

// ─�� Force-RDT: false skips minting on PII endpoint ─────────────────────────

func TestE2E_RDT_ForceHeader_False(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req.Header.Set("X-SP-Proxy-Force-RDT", "false")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	// No minting despite PII endpoint
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
	// Original token forwarded
	last := env.MockSPAPI.LastRequest()
	assert.Equal(t, "Atza|lwa-token", last.Header.Get("X-Amz-Access-Token"))
}

// ── Singleflight: concurrent requests produce only 1 mint ──────────────────

func TestE2E_RDT_Singleflight_ConcurrentMints(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	// Add a small delay to the Tokens API to ensure requests overlap
	env.MockSPAPI.SetLatency("/tokens/2021-03-01/restrictedDataToken", 100*time.Millisecond)
	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|shared-rdt"),
	})

	for _, orderId := range []string{"O1", "O2", "O3", "O4", "O5"} {
		env.MockSPAPI.SetResponse("/orders/v0/orders/"+orderId, mock.Response{
			StatusCode: 200,
			Headers:    jsonHeaders(),
			Body:       []byte(`{"payload":{"AmazonOrderId":"` + orderId + `"}}`),
		})
	}

	var wg sync.WaitGroup
	for _, orderId := range []string{"O1", "O2", "O3", "O4", "O5"} {
		wg.Add(1)
		go func(oid string) {
			defer wg.Done()
			req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/"+oid, nil)
			req.Header.Set("x-amz-access-token", "Atza|lwa-token")
			req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}(orderId)
	}
	wg.Wait()

	// Only 1 Tokens API call despite 5 concurrent requests
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Different Merchants: separate mints ────────────────────────────────────

func TestE2E_RDT_DifferentMerchants_SeparateMints(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|rdt-for-seller"),
	})

	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"AmazonOrderId":"123"}}`),
	})

	client := &http.Client{}

	// Seller 1
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token-seller-1")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp1, _ := client.Do(req1)
	resp1.Body.Close()

	// Seller 2
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token-seller-2")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_2")
	resp2, _ := client.Do(req2)
	resp2.Body.Close()

	// 2 separate mints (different merchants)
	assert.Equal(t, 2, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Report Sniffing: full 3-step flow ──────────────────────────────────────

func TestE2E_RDT_ReportSniffing_RestrictedType(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	// Step 1: POST /reports -> returns reportId
	env.MockSPAPI.SetResponse("/reports/2021-06-30/reports", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportId":"report-E2E"}`),
	})

	// Step 2: GET /reports/{reportId} -> returns documentId + reportType
	env.MockSPAPI.SetResponse("/reports/2021-06-30/reports/report-E2E", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportId":"report-E2E","reportType":"GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING","reportDocumentId":"doc-E2E","processingStatus":"DONE"}`),
	})

	// Step 3: GET /documents/{docId} -> the actual download
	env.MockSPAPI.SetResponse("/reports/2021-06-30/documents/doc-E2E", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportDocumentId":"doc-E2E","url":"https://s3.amazonaws.com/bucket/report.tsv"}`),
	})

	// Tokens API for the report document mint
	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|report-rdt"),
	})

	client := &http.Client{}

	// Step 1: POST /reports
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/reports/2021-06-30/reports",
		strings.NewReader(`{"reportType":"GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING","marketplaceIds":["ATVPDKIKX0DER"]}`))
	req1.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 200, resp1.StatusCode)
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))

	// Step 2: GET /reports/{reportId}
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/reports/2021-06-30/reports/report-E2E", nil)
	req2.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode)
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))

	// Step 3: GET /documents/{docId} -> should trigger mint
	req3, _ := http.NewRequest("GET", env.ProxyURL+"/reports/2021-06-30/documents/doc-E2E", nil)
	req3.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()

	assert.Equal(t, 200, resp3.StatusCode)
	assert.Contains(t, string(body3), "doc-E2E")
	// Exactly 1 mint call for the document download
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Report Sniffing: non-restricted type -> no mint ────────────────────────

func TestE2E_RDT_ReportSniffing_NonRestrictedType(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/reports/2021-06-30/reports", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportId":"report-NR"}`),
	})

	env.MockSPAPI.SetResponse("/reports/2021-06-30/reports/report-NR", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportId":"report-NR","reportType":"GET_FLAT_FILE_OPEN_LISTINGS_DATA","reportDocumentId":"doc-NR","processingStatus":"DONE"}`),
	})

	env.MockSPAPI.SetResponse("/reports/2021-06-30/documents/doc-NR", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportDocumentId":"doc-NR","url":"https://s3.amazonaws.com/bucket/listings.tsv"}`),
	})

	client := &http.Client{}

	// Step 1
	req1, _ := http.NewRequest("POST", env.ProxyURL+"/reports/2021-06-30/reports",
		strings.NewReader(`{"reportType":"GET_FLAT_FILE_OPEN_LISTINGS_DATA","marketplaceIds":["ATVPDKIKX0DER"]}`))
	req1.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req1.Header.Set("Content-Type", "application/json")
	resp1, _ := client.Do(req1)
	resp1.Body.Close()

	// Step 2
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/reports/2021-06-30/reports/report-NR", nil)
	req2.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp2, _ := client.Do(req2)
	resp2.Body.Close()

	// Step 3: should NOT mint
	req3, _ := http.NewRequest("GET", env.ProxyURL+"/reports/2021-06-30/documents/doc-NR", nil)
	req3.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req3.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp3, _ := client.Do(req3)
	resp3.Body.Close()

	assert.Equal(t, 200, resp3.StatusCode)
	// No Tokens API calls at all
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── Report: unknown documentId (skipped sniffing) -> passthrough ───────────

func TestE2E_RDT_ReportUnknownDocument_Passthrough(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/reports/2021-06-30/documents/unknown-doc", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"reportDocumentId":"unknown-doc","url":"https://s3.amazonaws.com/..."}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/reports/2021-06-30/documents/unknown-doc", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	// No mint since proxy doesn't know the reportType
	assert.Equal(t, 0, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
}

// ── MFN Endpoint: auto-mint works for non-Orders restricted endpoints ──────

func TestE2E_RDT_MFN_GetShipment(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|mfn-rdt"),
	})

	env.MockSPAPI.SetResponse("/mfn/v0/shipments/SHIP123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"ShipmentId":"SHIP123"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/mfn/v0/shipments/SHIP123", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/tokens/2021-03-01/restrictedDataToken"))
	last := env.MockSPAPI.LastRequest()
	assert.Equal(t, "Atz.sprdt|mfn-rdt", last.Header.Get("X-Amz-Access-Token"))
}

// ── Mint Request Validation: correct body sent to Tokens API ───────────────

func TestE2E_RDT_MintRequestBody_OrdersWithDataElements(t *testing.T) {
	env := NewTestEnv(t, WithRDTAutoMint(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/tokens/2021-03-01/restrictedDataToken", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       rdtTokenResponse("Atz.sprdt|rdt-with-data-elements"),
	})
	env.MockSPAPI.SetResponse("/orders/v0/orders/999", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/999", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Find the Tokens API request and verify its body
	tokenReq := env.MockSPAPI.FindRequest("/tokens/2021-03-01/restrictedDataToken")
	require.NotNil(t, tokenReq)

	var body struct {
		RestrictedResources []struct {
			Method       string   `json:"method"`
			Path         string   `json:"path"`
			DataElements []string `json:"dataElements"`
		} `json:"restrictedResources"`
	}
	err := json.Unmarshal(tokenReq.Body, &body)
	require.NoError(t, err)
	require.Len(t, body.RestrictedResources, 1)

	rr := body.RestrictedResources[0]
	assert.Equal(t, "GET", rr.Method)
	assert.Equal(t, "/orders/v0/orders/{orderId}", rr.Path)
	assert.ElementsMatch(t, []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"}, rr.DataElements)
}
