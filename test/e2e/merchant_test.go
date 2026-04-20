//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Priority 1: Explicit header resolution ───────────────────────────────────

func TestE2E_Merchant_ExplicitHeader(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/sellers/v1/marketplaceParticipations", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":[]}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/sellers/v1/marketplaceParticipations", nil)
	req.Header.Set("x-amz-access-token", "Atza|some-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "MY_EXPLICIT_SELLER")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "MY_EXPLICIT_SELLER", resp.Header.Get("X-SP-Proxy-Merchant-Key"))
}

// ── Priority 2: Config-based token mapping ───────────────────────────────────

func TestE2E_Merchant_TokenMapping(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithTokenMap(map[string]string{
			"Atza|mapped-token-123": "MAPPED_SELLER",
		}),
	)

	env.MockSPAPI.SetResponse("/sellers/v1/marketplaceParticipations", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":[]}`),
	})

	// No explicit merchant header, but token is in the map
	req, _ := http.NewRequest("GET", env.ProxyURL+"/sellers/v1/marketplaceParticipations", nil)
	req.Header.Set("x-amz-access-token", "Atza|mapped-token-123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "MAPPED_SELLER", resp.Header.Get("X-SP-Proxy-Merchant-Key"))
}

// ── Priority 3: SHA-256 hash fallback ────────────────────────────────────────

func TestE2E_Merchant_HashFallback(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/sellers/v1/marketplaceParticipations", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":[]}`),
	})

	token := "Atza|unknown-token-no-mapping"
	req, _ := http.NewRequest("GET", env.ProxyURL+"/sellers/v1/marketplaceParticipations", nil)
	req.Header.Set("x-amz-access-token", token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	// Compute expected hash
	hash := sha256.Sum256([]byte(token))
	expectedKey := "tokenhash:" + hex.EncodeToString(hash[:16])
	assert.Equal(t, expectedKey, resp.Header.Get("X-SP-Proxy-Merchant-Key"))
}

// ── Priority Order: explicit header wins over token map ──────────────────────

func TestE2E_Merchant_HeaderWinsOverTokenMap(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithTokenMap(map[string]string{
			"Atza|both-token": "TOKEN_MAP_SELLER",
		}),
	)

	env.MockSPAPI.SetResponse("/sellers/v1/marketplaceParticipations", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":[]}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/sellers/v1/marketplaceParticipations", nil)
	req.Header.Set("x-amz-access-token", "Atza|both-token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "EXPLICIT_SELLER")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Explicit header takes priority
	assert.Equal(t, "EXPLICIT_SELLER", resp.Header.Get("X-SP-Proxy-Merchant-Key"))
}

// ── Merchant Key Injection: response always includes merchant key header ─────

func TestE2E_Merchant_KeyInResponse(t *testing.T) {
	env := NewTestEnv(t, WithCacheDisabled(), WithRateLimitDisabled())

	env.MockSPAPI.SetResponse("/catalog/v0/items", mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body:       []byte(`{"payload":{}}`),
	})

	req, _ := http.NewRequest("GET", env.ProxyURL+"/catalog/v0/items", nil)
	req.Header.Set("x-amz-access-token", "Atza|token")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "RESPONSE_SELLER")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEmpty(t, resp.Header.Get("X-SP-Proxy-Merchant-Key"),
		"response must include X-SP-Proxy-Merchant-Key header")
}
