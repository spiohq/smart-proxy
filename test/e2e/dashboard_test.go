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

// helper: generate some traffic to populate logs
func generateTraffic(t *testing.T, env *TestEnv, count int) {
	t.Helper()
	client := &http.Client{}
	paths := []string{"traffic-A", "traffic-B", "traffic-C", "traffic-D", "traffic-E"}
	for i := 0; i < count; i++ {
		path := paths[i%len(paths)]
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/"+path, nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_DASH")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}
	// Let the async logger flush (batch timer is 1s)
	time.Sleep(1500 * time.Millisecond)
}

// ── Log Query API: returns logs with filters ─────────────────────────────────

func TestE2E_Dashboard_LogQuery(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/log-test", mock.Response{
		StatusCode: 200, Headers: jsonHeaders(), Body: []byte(`{"payload":{}}`),
	})

	// Generate a request
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/log-test", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_LOG")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	time.Sleep(1500 * time.Millisecond) // let logger flush

	// Query logs - API returns {"rows": [...], "total": N}
	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_LOG&limit=10")
	require.NoError(t, err)
	defer logResp.Body.Close()

	assert.Equal(t, 200, logResp.StatusCode)

	var result struct {
		Rows  []map[string]any `json:"rows"`
		Total float64          `json:"total"`
	}
	body, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(body, &result)

	assert.GreaterOrEqual(t, len(result.Rows), 1)
	if len(result.Rows) > 0 {
		assert.Equal(t, "SELLER_LOG", result.Rows[0]["merchantKey"])
	}
}

// ── Log Detail API: returns individual log by ID ─────────────────────────────

func TestE2E_Dashboard_LogDetail(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/detail-test", mock.Response{
		StatusCode: 200, Headers: jsonHeaders(), Body: []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/detail-test", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_DETAIL")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	time.Sleep(1500 * time.Millisecond)

	// Get logs to find an ID
	logResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_DETAIL&limit=1")
	require.NoError(t, err)
	defer logResp.Body.Close()

	var listResult struct {
		Rows []map[string]any `json:"rows"`
	}
	body, _ := io.ReadAll(logResp.Body)
	json.Unmarshal(body, &listResult)
	require.GreaterOrEqual(t, len(listResult.Rows), 1)

	logID := listResult.Rows[0]["id"].(string)

	// Get detail
	detailResp, err := http.Get(env.DashURL + "/api/v1/logs/" + logID)
	require.NoError(t, err)
	defer detailResp.Body.Close()

	assert.Equal(t, 200, detailResp.StatusCode)

	var detail map[string]any
	body2, _ := io.ReadAll(detailResp.Body)
	json.Unmarshal(body2, &detail)
	assert.Equal(t, logID, detail["id"])
	assert.Equal(t, "SELLER_DETAIL", detail["merchantKey"])
}

// ── Merchant List API: returns known merchants ───────────────────────────────

func TestE2E_Dashboard_MerchantList(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200, Headers: jsonHeaders(), Body: []byte(`{"payload":{}}`),
	})

	client := &http.Client{}
	for _, seller := range []string{"SELLER_M1", "SELLER_M2"} {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", seller)
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	time.Sleep(1500 * time.Millisecond)

	resp, err := http.Get(env.DashURL + "/api/v1/merchants")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	// API returns {"merchants": [...]}
	var result struct {
		Merchants []string `json:"merchants"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)
	assert.GreaterOrEqual(t, len(result.Merchants), 2)
}

// ── Log Filter by Status ─────────────────────────────────────────────────────

func TestE2E_Dashboard_LogFilterByStatus(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/ok", mock.Response{
		StatusCode: 200, Headers: jsonHeaders(), Body: []byte(`{"payload":{}}`),
	})
	env.MockSPAPI.SetError("/orders/v0/orders/fail", 500)

	client := &http.Client{}

	// Generate 200 and 500
	for _, path := range []string{"/orders/v0/orders/ok", "/orders/v0/orders/fail"} {
		req, _ := http.NewRequest("GET", env.ProxyURL+path, nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_FILTER")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	time.Sleep(1500 * time.Millisecond)

	// Filter for 5xx
	resp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=SELLER_FILTER&status=5xx")
	require.NoError(t, err)
	defer resp.Body.Close()

	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	// Should only have the 500 entry
	for _, entry := range result.Rows {
		code, _ := entry["statusCode"].(float64)
		assert.GreaterOrEqual(t, code, float64(500))
	}
}
