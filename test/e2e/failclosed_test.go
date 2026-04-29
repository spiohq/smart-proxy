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

// TestE2E_FailClosed_UnknownEndpointExcludedFromCache verifies that, with
// SP_PROXY_PII_FAIL_CLOSED=true, a path that does not match any registered
// SP-API endpoint pattern is treated as PII: the response is excluded from
// the cache (X-SP-Proxy-Cache: PII_EXCLUDED) and the body is replaced with
// the {"redacted": true, ...} placeholder in the dashboard log detail.
func TestE2E_FailClosed_UnknownEndpointExcludedFromCache(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled(), WithPIIFailClosed())

	// Path that the endpoint classifier does not know.
	const unknownPath = "/futureapi/2030-01-01/secretwidgets"
	env.MockSPAPI.SetResponse(unknownPath, mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"sensitive":"never-cache-me"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+unknownPath, nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_FC")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Application still receives the original payload unchanged.
	assert.Contains(t, string(body), "never-cache-me")

	// Cache excluded the PII-flagged response.
	assert.Equal(t, "PII_EXCLUDED", resp.Header.Get("X-SP-Proxy-Cache"))

	// Wait for async logger.
	time.Sleep(1500 * time.Millisecond)

	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_FC&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()
	var list struct {
		Rows []map[string]any `json:"rows"`
	}
	logBody, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(logBody, &list)
	require.GreaterOrEqual(t, len(list.Rows), 1)

	piiRedacted, _ := list.Rows[0]["piiRedacted"].(bool)
	assert.True(t, piiRedacted, "fail-closed must mark unknown-endpoint logs as PII-redacted")

	// Body endpoint should return the redacted placeholder, not the raw body.
	logID := list.Rows[0]["id"].(string)
	bodyResp, err := http.Get(env.DashURL + "/api/v1/logs/" + logID + "/body")
	require.NoError(t, err)
	defer bodyResp.Body.Close()
	require.Equal(t, http.StatusOK, bodyResp.StatusCode, "body endpoint should serve the stored (redacted) body")
	var bodies map[string]json.RawMessage
	rawBody, _ := io.ReadAll(bodyResp.Body)
	json.Unmarshal(rawBody, &bodies)

	respBody := string(bodies["responseBody"])
	assert.Contains(t, respBody, `"redacted":true`,
		"unknown endpoint body must be replaced with the redaction placeholder; got %s", respBody)
	assert.NotContains(t, respBody, "never-cache-me",
		"raw payload must not appear in the stored log body")
}

// TestE2E_FailOpen_UnknownEndpointCached confirms the default fail-open
// behavior is unchanged: an unknown path is cached normally and its body is
// stored verbatim in the dashboard. This is the regression check that
// fail-closed is opt-in.
func TestE2E_FailOpen_UnknownEndpointCached(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled())

	const unknownPath = "/futureapi/2030-01-01/widgets"
	env.MockSPAPI.SetResponse(unknownPath, mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"data":"cache-me"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+unknownPath, nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_FO")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.NotEqual(t, "PII_EXCLUDED", resp.Header.Get("X-SP-Proxy-Cache"),
		"fail-open default must not exclude unknown endpoints from cache")

	time.Sleep(1500 * time.Millisecond)

	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_FO&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()
	var list struct {
		Rows []map[string]any `json:"rows"`
	}
	logBody, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(logBody, &list)
	require.GreaterOrEqual(t, len(list.Rows), 1)

	piiRedacted, _ := list.Rows[0]["piiRedacted"].(bool)
	assert.False(t, piiRedacted, "fail-open default must not flag unknown endpoints as PII")
}

// TestE2E_FailClosed_KnownNonPIIParameterizedEndpointStillCached confirms
// that fail-closed does not over-redact: a known, parameterized SP-API path
// with no PII rules (e.g. catalog item by ASIN) continues to cache normally.
func TestE2E_FailClosed_KnownNonPIIParameterizedEndpointStillCached(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled(), WithPIIFailClosed())

	const knownPath = "/catalog/2022-04-01/items/B07XYZ123"
	env.MockSPAPI.SetResponse(knownPath, mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"asin":"B07XYZ123"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+knownPath, nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_FC_KNOWN")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	cacheStatus := resp.Header.Get("X-SP-Proxy-Cache")
	assert.NotEqual(t, "PII_EXCLUDED", cacheStatus,
		"known parameterized non-PII endpoint must NOT be excluded from cache in fail-closed mode (got %q)", cacheStatus)
}

// TestE2E_FailClosed_KnownFullBodyPIIStillRedacted confirms that fail-closed
// does not interfere with existing full-body PII handling: buyerInfo still
// gets the redaction placeholder.
func TestE2E_FailClosed_KnownFullBodyPIIStillRedacted(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled(), WithPIIFailClosed())

	env.MockSPAPI.SetResponse("/orders/v0/orders/123-456-789/buyerInfo", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"BuyerEmail":"buyer@example.com","BuyerName":"Jane Buyer"}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123-456-789/buyerInfo", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_FC_FB")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// App still gets the unredacted response.
	assert.Contains(t, string(body), "buyer@example.com")

	time.Sleep(1500 * time.Millisecond)

	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_FC_FB&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()
	var list struct {
		Rows []map[string]any `json:"rows"`
	}
	logBody, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(logBody, &list)
	require.GreaterOrEqual(t, len(list.Rows), 1)

	logID := list.Rows[0]["id"].(string)
	bodyResp, err := http.Get(env.DashURL + "/api/v1/logs/" + logID + "/body")
	require.NoError(t, err)
	defer bodyResp.Body.Close()
	require.Equal(t, http.StatusOK, bodyResp.StatusCode)
	var bodies map[string]json.RawMessage
	rawBody, _ := io.ReadAll(bodyResp.Body)
	json.Unmarshal(rawBody, &bodies)

	respBody := string(bodies["responseBody"])
	assert.Contains(t, respBody, `"redacted":true`)
	assert.NotContains(t, respBody, "buyer@example.com")
}
