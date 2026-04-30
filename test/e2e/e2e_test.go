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

func TestE2E_ProxyRoundTrip(t *testing.T) {
	env := NewTestEnv(t)

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"x-amzn-RequestId":       []string{"amz-123"},
			"x-amzn-RateLimit-Limit": []string{"0.0167"},
		},
		Body: []byte(`{"payload":{"orders":[]}}`),
	})

	resp, err := http.Get(env.ProxyURL + "/orders/v0/orders")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"orders"`)

	last := env.MockSPAPI.LastRequest()
	require.NotNil(t, last)
	assert.Equal(t, "/orders/v0/orders", last.Path)
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/orders/v0/orders"))
}

func TestE2E_CacheHitMiss(t *testing.T) {
	env := NewTestEnv(t)

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"x-amzn-RequestId":       []string{"amz-456"},
			"x-amzn-RateLimit-Limit": []string{"2.0"},
		},
		Body: []byte(`{"payload":{"items":["item1"]}}`),
	})

	resp1, err := http.Get(env.ProxyURL + "/catalog/v0/items")
	require.NoError(t, err)
	resp1.Body.Close()
	assert.Equal(t, 200, resp1.StatusCode)

	resp2, err := http.Get(env.ProxyURL + "/catalog/v0/items")
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode)

	// Only 1 request should have reached the mock (second was cached)
	assert.Equal(t, 1, env.MockSPAPI.RequestCount("/catalog/v0/items"))
}

func TestE2E_UpstreamError(t *testing.T) {
	env := NewTestEnv(t)
	env.MockSPAPI.SetError("/orders/v0/orders", 500)

	resp, err := http.Get(env.ProxyURL + "/orders/v0/orders")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 500, resp.StatusCode)
}

func TestE2E_DashboardHealth(t *testing.T) {
	env := NewTestEnv(t)

	resp, err := http.Get(env.DashURL + "/_sp-proxy/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "ok", body["status"])
}

func TestE2E_DashboardReady(t *testing.T) {
	env := NewTestEnv(t)

	resp, err := http.Get(env.DashURL + "/_sp-proxy/ready")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "ready", body["status"])
}

func TestE2E_DashboardLogs(t *testing.T) {
	env := NewTestEnv(t)

	// Generate some data
	resp, err := http.Get(env.ProxyURL + "/orders/v0/orders")
	require.NoError(t, err)
	resp.Body.Close()

	logsResp, err := http.Get(env.DashURL + "/api/v1/logs")
	require.NoError(t, err)
	defer logsResp.Body.Close()
	assert.Equal(t, 200, logsResp.StatusCode)
}

// Task 7: Graceful shutdown test
func TestE2E_GracefulShutdown(t *testing.T) {
	env := NewTestEnv(t)

	env.MockSPAPI.SetLatency("/slow/endpoint", 500*time.Millisecond)
	env.MockSPAPI.SetResponse("/slow/endpoint", mock.Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"status":"completed"}`),
	})

	resultCh := make(chan int, 1)
	go func() {
		resp, err := http.Get(env.ProxyURL + "/slow/endpoint")
		if err != nil {
			resultCh <- 0
			return
		}
		defer resp.Body.Close()
		resultCh <- resp.StatusCode
	}()

	// Give request time to reach mock, then shutdown
	time.Sleep(100 * time.Millisecond)
	err := env.Server.Shutdown()
	require.NoError(t, err)

	// In-flight request should complete
	status := <-resultCh
	assert.Equal(t, 200, status)
}
