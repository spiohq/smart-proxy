//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spiohq/smart-proxy/internal/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalSpec is a tiny SP-API-like OpenAPI 3.0 spec used by validation E2E
// tests. It defines one endpoint with a required query parameter so tests can
// trigger both valid and invalid request paths without a real spec download.
const minimalSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Test SP-API", "version": "1"},
  "paths": {
    "/orders/v0/orders": {
      "get": {
        "operationId": "getOrders",
        "parameters": [
          {
            "name": "marketplaceIds",
            "in": "query",
            "required": true,
            "schema": {"type": "string"}
          }
        ],
        "responses": {"200": {"description": "OK"}}
      }
    },
    "/listings/2021-08-01/items/{sellerId}/{sku}": {
      "put": {
        "operationId": "putListingsItem",
        "parameters": [
          {"name": "sellerId", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "sku", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "marketplaceIds", "in": "query", "required": true, "schema": {"type": "string"}}
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["productType"],
                "properties": {
                  "productType": {"type": "string"}
                }
              }
            }
          }
        },
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`

// buildValidationRouter writes minimalSpec to a temp dir and loads it via the
// real LoadFromDir path so tests exercise the full middleware stack.
func buildValidationRouter(t *testing.T) validation.Router {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.json"), []byte(minimalSpec), 0o600))
	router, err := validation.LoadFromDir(dir)
	require.NoError(t, err)
	return router
}

// TestE2E_Validation_InvalidRequest_Returns400 verifies that a request missing
// a required query parameter is rejected by the proxy with 400 and an SP-API
// error envelope, and never reaches the upstream mock.
func TestE2E_Validation_InvalidRequest_Returns400(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithValidationRouter(buildValidationRouter(t)),
	)

	req, err := http.NewRequest(http.MethodGet, env.ProxyURL+"/orders/v0/orders", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "rejected", resp.Header.Get("X-SP-Proxy-Validation"))
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"code":"InvalidInput"`)
	assert.Contains(t, string(body), "marketplaceIds")

	// Upstream must not have received the request.
	assert.Empty(t, env.MockSPAPI.Requests)
}

// TestE2E_Validation_ValidRequest_PassesThrough verifies that a well-formed
// request passes validation and is forwarded to the upstream.
func TestE2E_Validation_ValidRequest_PassesThrough(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithValidationRouter(buildValidationRouter(t)),
	)

	req, err := http.NewRequest(http.MethodGet,
		env.ProxyURL+"/orders/v0/orders?marketplaceIds=ATVPDKIKX0DER", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-SP-Proxy-Validation"))
	assert.NotEmpty(t, env.MockSPAPI.Requests, "upstream must have received the request")
}

// TestE2E_Validation_SkipHeader_BypassesValidation verifies that sending
// X-SP-Proxy-Skip-Validation: true forwards even an otherwise-invalid request.
func TestE2E_Validation_SkipHeader_BypassesValidation(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithValidationRouter(buildValidationRouter(t)),
	)

	req, err := http.NewRequest(http.MethodGet, env.ProxyURL+"/orders/v0/orders", nil)
	require.NoError(t, err)
	req.Header.Set("X-SP-Proxy-Skip-Validation", "true")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-SP-Proxy-Validation"))
	assert.NotEmpty(t, env.MockSPAPI.Requests)
}

// TestE2E_Validation_SkipHeader_NonTrueValues_StillValidates confirms that
// only the exact string "true" skips validation; case variants do not.
func TestE2E_Validation_SkipHeader_NonTrueValues_StillValidates(t *testing.T) {
	nonTrueValues := []string{"True", "TRUE", "1", "yes"}
	for _, val := range nonTrueValues {
		t.Run("value="+val, func(t *testing.T) {
			env := NewTestEnv(t,
				WithCacheDisabled(),
				WithRateLimitDisabled(),
				WithValidationRouter(buildValidationRouter(t)),
			)

			req, err := http.NewRequest(http.MethodGet, env.ProxyURL+"/orders/v0/orders", nil)
			require.NoError(t, err)
			req.Header.Set("X-SP-Proxy-Skip-Validation", val)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "skip header %q must not bypass validation", val)
			assert.Equal(t, "rejected", resp.Header.Get("X-SP-Proxy-Validation"))
			assert.Empty(t, env.MockSPAPI.Requests)
		})
	}
}

// TestE2E_Validation_UnknownPath_PassesThrough verifies graceful degradation:
// endpoints not in the loaded specs are forwarded without validation.
func TestE2E_Validation_UnknownPath_PassesThrough(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithValidationRouter(buildValidationRouter(t)),
	)

	req, err := http.NewRequest(http.MethodGet, env.ProxyURL+"/someNewEndpoint/v99/items", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-SP-Proxy-Validation"))
	assert.NotEmpty(t, env.MockSPAPI.Requests)
}

// TestE2E_Validation_ErrorEnvelope_Shape validates that the 400 body is a
// well-formed SP-API error envelope with the expected fields.
func TestE2E_Validation_ErrorEnvelope_Shape(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithValidationRouter(buildValidationRouter(t)),
	)

	req, err := http.NewRequest(http.MethodGet, env.ProxyURL+"/orders/v0/orders", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var envelope struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details string `json:"details"`
		} `json:"errors"`
	}
	body, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.NotEmpty(t, envelope.Errors)
	assert.Equal(t, "InvalidInput", envelope.Errors[0].Code)
	assert.NotEmpty(t, envelope.Errors[0].Message)
	assert.Equal(t, "validated by proxy against SP-API OpenAPI spec", envelope.Errors[0].Details)
}

// TestE2E_Validation_DisabledByNilRouter_PassesThrough ensures that when no
// validation router is configured (nil), all requests pass through unchanged.
func TestE2E_Validation_DisabledByNilRouter_PassesThrough(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		// No WithValidationRouter -> nil router -> validation disabled
	)

	// This request is missing the required marketplaceIds parameter; without
	// validation it must reach the upstream and return 200.
	req, err := http.NewRequest(http.MethodGet, env.ProxyURL+"/orders/v0/orders", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-SP-Proxy-Validation"))

	_ = strings.NewReader("") // keep strings import used by other tests
}

// TestE2E_Validation_RequestBodyForwardedToUpstream verifies that a valid
// request body is not consumed by validation and still reaches the upstream.
// This is a regression test for the body-drain bug: ValidateRequest reads
// r.Body, so the middleware must restore it before calling next.
func TestE2E_Validation_RequestBodyForwardedToUpstream(t *testing.T) {
	env := NewTestEnv(t,
		WithCacheDisabled(),
		WithRateLimitDisabled(),
		WithValidationRouter(buildValidationRouter(t)),
	)

	const payload = `{"productType":"LUGGAGE"}`
	req, err := http.NewRequest(http.MethodPut,
		env.ProxyURL+"/listings/2021-08-01/items/SELLER1/SKU1?marketplaceIds=ATVPDKIKX0DER",
		strings.NewReader(payload),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, env.MockSPAPI.Requests, 1)
	assert.Equal(t, payload, string(env.MockSPAPI.Requests[0].Body),
		"request body must arrive at upstream unchanged after validation")
}
