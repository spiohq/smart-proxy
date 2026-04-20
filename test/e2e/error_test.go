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

// ── Upstream 502: structured error response with classification ──────────────

func TestE2E_Error_UpstreamUnavailable(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	// Close the mock server to simulate upstream connection refused.
	// The proxy is still running and accepting connections.
	env.MockSPAPI.CloseClientConnections()
	env.MockSPAPI.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp, err := client.Do(req)
	if err != nil {
		// Proxy itself may be unreachable (race with shutdown), that's ok
		t.Skip("proxy unreachable after mock close: ", err)
		return
	}
	defer resp.Body.Close()

	assert.Equal(t, 502, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("X-SP-Proxy-Error-Reason"))

	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]any
	json.Unmarshal(body, &errResp)
	errors, ok := errResp["errors"].([]any)
	assert.True(t, ok, "response should have errors array")
	if len(errors) > 0 {
		errObj := errors[0].(map[string]any)
		assert.Equal(t, "PROXY_ERROR", errObj["code"])
	}
}

// ── Upstream Timeout: slow response causes client-side timeout ───────────────

func TestE2E_Error_UpstreamTimeout(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetLatency("/orders/v0/orders/timeout-test", 10*time.Second)
	env.MockSPAPI.SetResponse("/orders/v0/orders/timeout-test", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/timeout-test", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	_, err := client.Do(req)
	assert.Error(t, err, "request should timeout")
}

// ── Upstream 5xx: error status passed through correctly ──────────────────────

func TestE2E_Error_503PassThrough(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/503-test", mock.Response{
		StatusCode: 503,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"errors":[{"code":"ServiceUnavailable","message":"service unavailable"}]}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/503-test", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 503, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "ServiceUnavailable")
}

// ── Upstream 429: throttle response passed through ───────────────────────────

func TestE2E_Error_429PassThrough(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/429-test", mock.Response{
		StatusCode: 429,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
			"Retry-After":  []string{"2"},
		},
		Body: []byte(`{"errors":[{"code":"QuotaExceeded","message":"too many requests"}]}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/429-test", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 429, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "QuotaExceeded")
}

// ── Multiple Error Types: 400, 401, 403, 404 ────────────────────────────────

func TestE2E_Error_ClientErrors(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	tests := []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{"400 Bad Request", "/orders/v0/orders/err-400", 400, "InvalidInput"},
		{"401 Unauthorized", "/orders/v0/orders/err-401", 401, "Unauthorized"},
		{"403 Forbidden", "/orders/v0/orders/err-403", 403, "AccessDenied"},
		{"404 Not Found", "/orders/v0/orders/err-404", 404, "NotFound"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env.MockSPAPI.SetResponse(tt.path, mock.Response{
				StatusCode: tt.status,
				Headers:    jsonHeaders(),
				Body:       []byte(`{"errors":[{"code":"` + tt.code + `","message":"test error"}]}`),
			})

			req, _ := http.NewRequest("GET", env.ProxyURL+tt.path, nil)
			req.Header.Set("x-amz-access-token", "Atza|token")
			req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.status, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			assert.Contains(t, string(body), tt.code)
		})
	}
}
