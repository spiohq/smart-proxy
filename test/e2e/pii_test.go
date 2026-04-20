//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- PII Redaction in Logs: buyer info redacted --

func TestE2E_PII_BuyerInfoRedactedInLogs(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	// Response contains PII buyer info
	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body: []byte(`{"payload":{"Orders":[{
			"AmazonOrderId":"111-222-333",
			"BuyerInfo":{"BuyerEmail":"secret@buyer.com","BuyerName":"John Doe"},
			"ShippingAddress":{"Name":"John Doe","AddressLine1":"123 Secret St","City":"Anytown","PostalCode":"12345","Phone":"555-1234"}
		}]}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_PII")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// The response to the client should contain the original (unredacted) data
	assert.Contains(t, string(body), "secret@buyer.com")
	assert.Contains(t, string(body), "John Doe")

	// Wait for async logger
	time.Sleep(1500 * time.Millisecond)

	// Check the stored log entry via dashboard - API returns {"rows": [...]}
	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_PII&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()

	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	logBody, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(logBody, &result)
	require.GreaterOrEqual(t, len(result.Rows), 1)

	// The log should have the PIIRedacted flag
	piiRedacted, _ := result.Rows[0]["piiRedacted"].(bool)
	assert.True(t, piiRedacted, "PII should be marked as redacted in logs")
}

// -- Authorization Header Redacted: auth tokens not stored in logs --

func TestE2E_PII_AuthHeaderRedacted(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200, Headers: jsonHeaders(), Body: []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req.Header.Set("x-amz-access-token", "Atza|super-secret-token-12345")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_AUTH")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	time.Sleep(1500 * time.Millisecond)

	// Get the log list - API returns {"rows": [...]}
	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_AUTH&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()

	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	logBody, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(logBody, &result)
	require.GreaterOrEqual(t, len(result.Rows), 1)

	logID := result.Rows[0]["id"].(string)

	// Get detail with headers
	detailResp, err := http.Get(env.DashURL + "/api/v1/logs/" + logID)
	require.NoError(t, err)
	defer detailResp.Body.Close()

	var detail map[string]any
	detailBody, _ := io.ReadAll(detailResp.Body)
	json.Unmarshal(detailBody, &detail)

	// Request headers should not contain the raw access token
	reqHeaders, _ := detail["requestHeaders"].(map[string]any)
	if reqHeaders != nil {
		for _, val := range reqHeaders {
			valStr, ok := val.(string)
			if ok {
				assert.NotContains(t, valStr, "super-secret-token-12345",
					"raw access token should be redacted in stored headers")
			}
		}
	}
}

// -- PII Endpoint Detection: orders with dataElements recognized as PII --

func TestE2E_PII_DataElementsDetected(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"Orders":[{"AmazonOrderId":"111"}]}}`),
	})
	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"items":[]}}`),
	})

	client := &http.Client{}

	// Request with buyerInfo dataElement (PII)
	req1, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders?dataElements=buyerInfo", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token")
	req1.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_PII_DE")
	resp1, err := client.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()

	// Should be PII_EXCLUDED from cache
	assert.Equal(t, "PII_EXCLUDED", resp1.Header.Get("X-SP-Proxy-Cache"))

	// Request to a non-PII endpoint (catalog - no PII)
	req2, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items?keywords=test", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token")
	req2.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_PII_DE")
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()

	// Should be a normal cache MISS (not PII excluded)
	assert.Equal(t, "MISS", resp2.Header.Get("X-SP-Proxy-Cache"))
}

// -- Non-PII Endpoint: no PII flag in logs --

func TestE2E_PII_NonPIIEndpointNotFlagged(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/definitions/2020-09-01/productTypes", mock.Response{
		StatusCode: 200, Headers: jsonHeaders(), Body: []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/definitions/2020-09-01/productTypes", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_NOPII")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	time.Sleep(1500 * time.Millisecond)

	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_NOPII&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()

	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	logBody, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(logBody, &result)

	if len(result.Rows) > 0 {
		piiRedacted, _ := result.Rows[0]["piiRedacted"].(bool)
		assert.False(t, piiRedacted, "non-PII endpoint should not have PII flag")
	}
}
