//go:build e2e

package e2e

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Reject Mode: immediate 429 when bucket is empty ──────────────────────────

func TestE2E_RateLimit_RejectMode(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitMode("reject"), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Exhaust the burst capacity (burst=20 for /orders/v0/orders)
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_REJECT")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, 200, resp.StatusCode)
	}

	// Next request should be rejected immediately with 429
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_REJECT")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 429, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Retry-After"))
}

// ── Queue Mode: request waits for token and succeeds ─────────────────────────

func TestE2E_RateLimit_QueueMode(t *testing.T) {
	// Use a low-rate endpoint so it's easy to exhaust
	env := NewTestEnv(t, WithRateLimitMode("queue"), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"x-amzn-RateLimit-Limit": []string{"0.0167"},
		},
		Body: []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Exhaust burst
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_QUEUE")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Next request should be queued, eventually succeed
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_QUEUE")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "true", resp.Header.Get("X-SP-Proxy-Queued"))
	assert.NotEmpty(t, resp.Header.Get("X-SP-Proxy-Queue-Wait-Ms"))
}

// ── Queue-Timeout Mode: request times out and returns 429 ────────────────────

func TestE2E_RateLimit_QueueTimeoutMode(t *testing.T) {
	// Very short timeout so it fires quickly
	env := NewTestEnv(t, WithRateLimitMode("queue-timeout"), WithQueueTimeout("100ms"), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"x-amzn-RateLimit-Limit": []string{"0.0167"},
		},
		Body: []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Exhaust burst
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_QT")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Next request should queue, then timeout and return 429
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_QT")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 429, resp.StatusCode)
	assert.Equal(t, "true", resp.Header.Get("X-SP-Proxy-Queued"))
}

// ── Rate Limit Headers: remaining tokens and queued flag ─────────────────────

func TestE2E_RateLimit_ResponseHeaders(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders/123", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_HDR")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	// Non-queued request should have queued=false and remaining tokens
	assert.Equal(t, "false", resp.Header.Get("X-SP-Proxy-Queued"))
	assert.NotEmpty(t, resp.Header.Get("X-SP-Proxy-Rate-Limit-Remaining"))
}

// ── Unknown Endpoint: rate limiting bypassed ─────────────────────────────────

func TestE2E_RateLimit_UnknownEndpointBypassed(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitMode("reject"), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/some/unknown/endpoint", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/some/unknown/endpoint", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "false", resp.Header.Get("X-SP-Proxy-Rate-Limit-Active"))
}

// ── Throttle Mode Resolution: header overrides config ────────────────────────

func TestE2E_RateLimit_HeaderOverridesMode(t *testing.T) {
	// Default mode is "queue", but header says "reject"
	env := NewTestEnv(t, WithRateLimitMode("queue"), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"x-amzn-RateLimit-Limit": []string{"0.0167"},
		},
		Body: []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Exhaust burst
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_OVERRIDE")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Request with reject header should get 429 immediately
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_OVERRIDE")
	req.Header.Set("X-SP-Proxy-Throttle-Mode", "reject")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 429, resp.StatusCode)
}

// ── Per-Merchant Mode: merchant config overrides global ──────────────────────

func TestE2E_RateLimit_MerchantModeOverride(t *testing.T) {
	env := NewTestEnv(t,
		WithRateLimitMode("queue"),
		WithMerchantModes(map[string]string{"SELLER_REJECT_ME": "reject"}),
		WithCacheDisabled(),
	)

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"x-amzn-RateLimit-Limit": []string{"0.0167"},
		},
		Body: []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Exhaust burst for SELLER_REJECT_ME
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_REJECT_ME")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Should be rejected (merchant-specific mode=reject), not queued
	req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_REJECT_ME")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 429, resp.StatusCode)
}

// ── Per-Merchant Bucket Isolation: separate buckets per merchant ──────────────

func TestE2E_RateLimit_MerchantIsolation(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitMode("reject"), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Exhaust burst for SELLER_A
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token-a")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_A")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// SELLER_A should be rate limited
	reqA, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	reqA.Header.Set("x-amz-access-token", "Atza|token-a")
	reqA.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_A")
	respA, err := client.Do(reqA)
	require.NoError(t, err)
	respA.Body.Close()
	assert.Equal(t, 429, respA.StatusCode)

	// SELLER_B should still be fine (separate bucket)
	reqB, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
	reqB.Header.Set("x-amz-access-token", "Atza|token-b")
	reqB.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_B")
	respB, err := client.Do(reqB)
	require.NoError(t, err)
	respB.Body.Close()
	assert.Equal(t, 200, respB.StatusCode)
}

// ── Concurrent Requests in Queue: multiple requests queued simultaneously ────

func TestE2E_RateLimit_ConcurrentQueue(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitMode("queue"), WithCacheDisabled())

	// Use a high-rate endpoint so queued requests resolve quickly
	env.MockSPAPI.SetResponse("/shipping/v1/shipments/S1", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	client := &http.Client{Timeout: 10 * time.Second}

	// Exhaust burst (burst=15 for shipping)
	for i := 0; i < 15; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/shipping/v1/shipments/S1", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_CONC")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Send 3 concurrent requests - all should eventually succeed
	var wg sync.WaitGroup
	var successCount atomic.Int32
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", env.ProxyURL+"/shipping/v1/shipments/S1", nil)
			req.Header.Set("x-amz-access-token", "Atza|token")
			req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_CONC")
			resp, err := client.Do(req)
			if err == nil {
				if resp.StatusCode == 200 {
					successCount.Add(1)
				}
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(3), successCount.Load(), "all queued requests should eventually succeed")
}

// ── Rate Limit Disabled: no throttling ───────────────────────────────────────

func TestE2E_RateLimit_Disabled(t *testing.T) {
	env := NewTestEnv(t, WithRateLimitDisabled(), WithCacheDisabled())

	env.MockSPAPI.SetResponse("/orders/v0/orders", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{"orders":[]}}`),
	})

	client := &http.Client{}

	// Send many requests - none should be rate limited
	for i := 0; i < 30; i++ {
		req, _ := http.NewRequest("GET", env.ProxyURL+"/orders/v0/orders", nil)
		req.Header.Set("x-amz-access-token", "Atza|token")
		req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_NOLIMIT")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, 200, resp.StatusCode, "request %d should not be rate limited", i)
	}
}
