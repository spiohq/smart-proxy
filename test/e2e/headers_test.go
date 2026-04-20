//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── X-SP-Proxy-* headers stripped before upstream ────────────────────────────

func TestE2E_Headers_ProxyHeadersStripped(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req.Header.Set("X-SP-Proxy-No-Cache", "true")
	req.Header.Set("X-SP-Proxy-Cache-Key", "custom")
	req.Header.Set("X-SP-Proxy-Cache-TTL", "10s")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")
	req.Header.Set("X-SP-Proxy-Throttle-Mode", "reject")
	req.Header.Set("X-SP-Proxy-Custom-Header", "should-be-stripped")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	// Verify all X-SP-Proxy-* headers were stripped from the upstream request
	last := env.MockSPAPI.LastRequest()
	require.NotNil(t, last)

	for key := range last.Header {
		assert.NotContains(t, key, "X-Sp-Proxy-",
			"proxy header %s should be stripped before reaching upstream", key)
	}
}

// ── Non-proxy headers preserved ──────────────────────────────────────────────

func TestE2E_Headers_NonProxyHeadersPreserved(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Custom-Header", "keep-me")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	last := env.MockSPAPI.LastRequest()
	require.NotNil(t, last)

	// Access token should be forwarded
	assert.NotEmpty(t, last.Header.Get("X-Amz-Access-Token"))
	// Custom headers should be preserved
	assert.Equal(t, "keep-me", last.Header.Get("X-Custom-Header"))
	assert.Equal(t, "application/json", last.Header.Get("Accept"))
}

// ── Upstream response headers passed through to client ────────────────────────

func TestE2E_Headers_UpstreamResponseHeaders(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"Content-Type":            []string{"application/json"},
			"x-amzn-RequestId":       []string{"amz-req-12345"},
			"x-amzn-RateLimit-Limit": []string{"2.0"},
			"X-Custom-Response":      []string{"from-upstream"},
		},
		Body: []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_1")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.Equal(t, "amz-req-12345", resp.Header.Get("X-Amzn-Requestid"))
	assert.Equal(t, "from-upstream", resp.Header.Get("X-Custom-Response"))
}

// ── Merchant key header always present in response ───────────────────────────

func TestE2E_Headers_MerchantKeyInResponse(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_HEADER_TEST")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "SELLER_HEADER_TEST", resp.Header.Get("X-SP-Proxy-Merchant-Key"))
}
