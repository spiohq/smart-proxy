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
	"time"

	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_DPP_NoLeakOfBuyerEmail is the canonical proof that PII does not
// leak into Smart Proxy storage when:
//
//  1. The client sends a PII filter in the query string (?buyerEmail=...)
//  2. The upstream response also contains PII fields (BuyerInfo, ShippingAddress)
//
// Verifies four invariants in a single run:
//   - The client receives the unredacted upstream response (forwarding works).
//   - The PII response is excluded from cache (X-SP-Proxy-Cache: PII_EXCLUDED).
//   - SQLite request_logs.query_params (via the dashboard API) contains no
//     cleartext PII filter values.
//   - The on-disk JSONL body file contains no cleartext PII (neither the
//     upstream BuyerEmail nor the filter buyerEmail).
//
// This is the test referenced by docs/DPP_COMPLIANCE.md as the no-leak proof.
func TestE2E_DPP_NoLeakOfBuyerEmail(t *testing.T) {
	const (
		upstreamEmail = "buyer-pii-canary@example.com"
		filterEmail   = "filter-pii-canary@example.com"
		ordersPath    = "/orders/v0/orders"
	)

	env := NewTestEnv(t, WithRateLimitDisabled())

	// Mock Amazon backend returns a body containing cleartext PII at every
	// field a v0 list-orders response would carry.
	env.MockSPAPI.SetResponse(ordersPath, mock.Response{
		StatusCode: 200,
		Headers:    jsonHeaders(),
		Body: []byte(`{"payload":{"Orders":[{"AmazonOrderId":"902-1234567-1234567",` +
			`"BuyerInfo":{"BuyerEmail":"` + upstreamEmail + `","BuyerName":"Jane Doe"},` +
			`"ShippingAddress":{"Name":"Jane Doe","AddressLine1":"1 Main St"}}]}}`),
	})

	// Client request: PII in BOTH the query string filter and (eventually)
	// the upstream response body.
	url := env.ProxyURL + ordersPath +
		"?buyerEmail=" + filterEmail + "&MarketplaceIds=A1PA6795UKMFR9"
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("x-amz-access-token", "Atza|fake")
	req.Header.Set("X-SP-Proxy-Merchant-Id", "DPP_CANARY")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)

	// (1) Forwarding: client receives the cleartext upstream response.
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), upstreamEmail,
		"client must receive the unredacted upstream response")

	// (2) Cache: PII response is excluded.
	assert.Equal(t, "PII_EXCLUDED", resp.Header.Get("X-SP-Proxy-Cache"))

	// Wait for the async logger to flush to SQLite + JSONL.
	time.Sleep(1500 * time.Millisecond)

	// (3) SQLite query_params (via dashboard detail API): no cleartext PII.
	// The list endpoint does not expose queryParams; the detail endpoint does.
	listResp, err := http.Get(env.DashURL + "/api/v1/logs?merchant=DPP_CANARY&limit=5")
	require.NoError(t, err)
	var list struct {
		Rows []map[string]any `json:"rows"`
	}
	listBody, err := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(listBody, &list))
	require.GreaterOrEqual(t, len(list.Rows), 1, "expected at least one log row for DPP_CANARY")

	logID, _ := list.Rows[0]["id"].(string)
	require.NotEmpty(t, logID, "log row must have an id")

	detailResp, err := http.Get(env.DashURL + "/api/v1/logs/" + logID)
	require.NoError(t, err)
	defer detailResp.Body.Close()
	require.Equal(t, http.StatusOK, detailResp.StatusCode)

	var detail map[string]any
	detailBody, err := io.ReadAll(detailResp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(detailBody, &detail))

	queryParams, _ := detail["queryParams"].(string)
	assert.NotContains(t, queryParams, filterEmail,
		"query_params must NOT contain cleartext buyer email; got %q", queryParams)
	assert.Contains(t, queryParams, "buyerEmail=%5BREDACTED%5D",
		"query_params must contain the redaction marker for buyerEmail")
	assert.Contains(t, queryParams, "MarketplaceIds=A1PA6795UKMFR9",
		"non-PII query params must remain visible for debugging")

	// (4) JSONL body files: no cleartext PII anywhere.
	bodiesRoot := env.Config.Bodies.BasePath
	matches, err := filepath.Glob(filepath.Join(bodiesRoot, "current", "*.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, matches, "expected at least one JSONL body file under %s/current/", bodiesRoot)

	for _, p := range matches {
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		s := string(raw)

		assert.NotContains(t, s, upstreamEmail,
			"JSONL must not contain cleartext upstream BuyerEmail (file=%s)", p)
		assert.NotContains(t, s, filterEmail,
			"JSONL must not contain cleartext filter email (file=%s)", p)

		// Sanity check: each line is valid JSON.
		for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			if line == "" {
				continue
			}
			var entry map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(line), &entry),
				"JSONL line must be valid JSON (file=%s, line=%s)", p, line)
		}
	}
}
