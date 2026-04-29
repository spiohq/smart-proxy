package logging

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestLogger(t *testing.T) (*AsyncLogger, *mockStore, *mockBodyStore) {
	t.Helper()
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())
	logger := NewAsyncLogger(ms, bs, engine, 100)
	t.Cleanup(func() { logger.Close() })
	return logger, ms, bs
}

func withMerchant(r *http.Request, key string) *http.Request {
	ctx := merchant.ContextWithMerchant(r.Context(), merchant.ResolvedMerchant{Key: key})
	return r.WithContext(ctx)
}

func TestLoggingMiddleware_BasicCapture(t *testing.T) {
	logger, ms, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-amz-request-id", "amz-123")
		w.WriteHeader(200)
		w.Write([]byte(`{"data":"test"}`))
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/orders/v0/orders?status=Shipped", nil)
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// Response should pass through to client
	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, `{"data":"test"}`, rec.Body.String())

	// Close to flush
	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	meta := allMeta[0]
	assert.NotEmpty(t, meta.ID)
	assert.Equal(t, "merchant-a", meta.MerchantKey)
	assert.Equal(t, "eu", meta.Region)
	assert.Equal(t, "GET", meta.Method)
	assert.Equal(t, "/orders/v0/orders", meta.Path)
	assert.Equal(t, "status=Shipped", meta.QueryParams)
	assert.Equal(t, 200, meta.StatusCode)
	assert.Equal(t, "amz-123", meta.AmazonRequestID)
	assert.Greater(t, meta.TotalLatencyMs, int64(-1))

	// Body should be stored
	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.Equal(t, meta.ID, allBodies[0].ID)
	assert.Contains(t, string(allBodies[0].ResponseBody), "test")
}

func TestLoggingMiddleware_CacheHit(t *testing.T) {
	logger, ms, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Cache", "HIT")
		w.Header().Set("X-SP-Proxy-Cache-Source-ID", "original-req-001")
		w.WriteHeader(200)
		w.Write([]byte(`{"cached":"data"}`))
	})

	mw := LoggingMiddleware(logger, registry, "na", 0)(handler)

	req := httptest.NewRequest("GET", "/catalog/2022-04-01/items/B123", nil)
	req = withMerchant(req, "merchant-b")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.Equal(t, "HIT", allMeta[0].CacheStatus)
	assert.Equal(t, "original-req-001", allMeta[0].CachedFromID)

	// Cache hits write a headers-only body entry (no payload, but headers still
	// need to be retrievable from the JSONL file).
	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.Empty(t, allBodies[0].RequestBody)
	assert.Empty(t, allBodies[0].ResponseBody)
}

func TestLoggingMiddleware_PIIEndpoint(t *testing.T) {
	logger, ms, _ := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"payload":{"Orders":[{"BuyerInfo":{"BuyerEmail":"secret@test.com"}}]}}`))
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// Client gets unredacted response
	assert.Contains(t, rec.Body.String(), "secret@test.com")

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.True(t, allMeta[0].PIIRedactedResponse)
}

func TestLoggingMiddleware_RedactsRequestHeaders(t *testing.T) {
	logger, _, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Custom", "visible")
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.Equal(t, "[REDACTED]", allBodies[0].RequestHeaders["Authorization"])
	assert.Equal(t, "visible", allBodies[0].RequestHeaders["X-Custom"])
}

func TestLoggingMiddleware_RedactsResponseHeaders(t *testing.T) {
	// F-12: Set-Cookie (and any other SensitiveHeaders entry) coming back
	// from upstream must be redacted before persistence, mirror of the
	// request-side test above.
	logger, _, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "session=secret-cookie-value")
		w.Header().Set("X-Public", "ok")
		w.WriteHeader(200)
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req = withMerchant(req, "merchant-a")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.Equal(t, "[REDACTED]", allBodies[0].ResponseHeaders["Set-Cookie"])
	assert.Equal(t, "ok", allBodies[0].ResponseHeaders["X-Public"])
}

func TestLoggingMiddleware_CapsCapturedBodies(t *testing.T) {
	logger, _, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	const cap = 64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(bytes.Repeat([]byte("A"), 4096))
	})

	mw := LoggingMiddleware(logger, registry, "eu", cap)(handler)

	reqBody := strings.Repeat("B", 4096)
	req := httptest.NewRequest("POST", "/test", strings.NewReader(reqBody))
	req.ContentLength = int64(len(reqBody))
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	// The request was 4 KiB but the cap was 64 bytes, so the middleware
	// must not retain the oversized payload.
	assert.LessOrEqual(t, len(allBodies[0].RequestBody), cap,
		"request body must be dropped or truncated when ContentLength exceeds cap")
	assert.LessOrEqual(t, len(allBodies[0].ResponseBody), cap,
		"response body must be truncated to cap")
}

func TestLoggingMiddleware_SetsRequestIDInContext(t *testing.T) {
	logger, _, _ := setupTestLogger(t)
	registry := pii.NewRegistry()

	var capturedID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = cache.RequestIDFromContext(r.Context())
		w.WriteHeader(200)
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	assert.NotEmpty(t, capturedID)
	assert.Len(t, capturedID, 32) // 16 bytes = 32 hex chars
}

func TestLoggingMiddleware_QueuedRequest(t *testing.T) {
	logger, ms, _ := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Queued", "true")
		w.Header().Set("X-SP-Proxy-Queue-Wait-Ms", "150")
		w.WriteHeader(200)
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.True(t, allMeta[0].Queued)
	assert.Equal(t, int64(150), allMeta[0].QueueWaitMs)
}

func TestLoggingMiddleware_ClientDisconnected_NotLogged(t *testing.T) {
	logger, ms, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-SP-Proxy-Error-Reason", "client_disconnected")
		w.WriteHeader(502)
		w.Write([]byte(`{"errors":[{"code":"PROXY_ERROR","message":"upstream unavailable","detail":"client_disconnected"}]}`))
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", nil)
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// Response still reaches the client
	assert.Equal(t, 502, rec.Code)

	logger.Close()

	// But nothing should be logged
	assert.Empty(t, ms.allEntries(), "client_disconnected requests should not be logged")
	assert.Empty(t, bs.allEntries(), "client_disconnected requests should not store bodies")
}

func TestLoggingMiddleware_MerchantResolverBeforeLogger(t *testing.T) {
	// Simulates the correct middleware chain: Resolver -> Logger -> Handler
	// The merchant header must be resolved and available in the log entry.
	logger, ms, _ := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	})

	// Build middleware chain: resolver outermost, then logger, then handler
	resolver := merchant.NewResolver(nil)
	chain := resolver.Middleware()(LoggingMiddleware(logger, registry, "eu", 0)(handler))

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "SELLER_TEST_123")

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.Equal(t, "SELLER_TEST_123", allMeta[0].MerchantKey,
		"merchant must be resolved from X-SP-Proxy-Merchant-Id header before logging")
}

func TestLoggingMiddleware_MerchantFallbackToTokenHash(t *testing.T) {
	// Without X-SP-Proxy-Merchant-Id, falls back to token hash
	logger, ms, _ := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	resolver := merchant.NewResolver(nil)
	chain := resolver.Middleware()(LoggingMiddleware(logger, registry, "eu", 0)(handler))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|some-token")

	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.Contains(t, allMeta[0].MerchantKey, "tokenhash:",
		"without explicit merchant header, should fallback to token hash")
}

func TestLoggingMiddleware_QueryParamsRedacted(t *testing.T) {
	logger, ms, _ := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	})
	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/orders/v0/orders?buyerEmail=foo%40bar.com&MarketplaceIds=A1", nil)
	req = withMerchant(req, "merchant-a")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	got := allMeta[0].QueryParams
	assert.NotContains(t, got, "foo%40bar.com", "buyerEmail value must be redacted in stored query params")
	assert.NotContains(t, got, "foo@bar.com")
	assert.Contains(t, got, "buyerEmail=%5BREDACTED%5D")
	assert.Contains(t, got, "MarketplaceIds=A1", "non-PII params must remain visible")
}

func TestLoggingMiddleware_QueryParamsRedacted_CustomExtras(t *testing.T) {
	t.Helper()
	ms := &mockStore{}
	bs := &mockBodyStore{}
	registry := pii.NewRegistryWithExtras([]string{"customField"})
	engine := pii.NewEngine(registry)
	logger := NewAsyncLogger(ms, bs, engine, 100)
	t.Cleanup(func() { logger.Close() })

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	req := httptest.NewRequest("GET", "/catalog/2022-04-01/items?customField=secret&asin=B0", nil)
	req = withMerchant(req, "merchant-b")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	got := allMeta[0].QueryParams
	assert.Contains(t, got, "customField=%5BREDACTED%5D")
	assert.Contains(t, got, "asin=B0")
}

func TestLoggingMiddleware_PostMessagingRequestBodyRedacted(t *testing.T) {
	logger, ms, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain body to mimic upstream consumption
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})

	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	body := `{"message":{"text":"Hi Maria, see you at Hauptstrasse 42, 10115 Berlin."}}`
	req := httptest.NewRequest("POST",
		"/messaging/v1/orders/903-3489051-5871062/messages/createConfirmServiceDetails",
		strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.NotContains(t, string(allBodies[0].RequestBody), "Maria",
		"messaging request body must be redacted before persistence (F-02)")
	assert.NotContains(t, string(allBodies[0].RequestBody), "Hauptstrasse")
	assert.Contains(t, string(allBodies[0].RequestBody), "REDACTED")

	// Metadata flag assertion: PIIRedactedRequest must be true so downstream
	// consumers (F-15 read-side filter, audit, dashboard) can rely on it.
	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.True(t, allMeta[0].PIIRedactedRequest,
		"messaging POST must set PIIRedactedRequest=true in metadata")
	assert.False(t, allMeta[0].PIIRedactedResponse,
		"messaging POST does not redact response (no full-body GET match)")
}

func TestLoggingMiddleware_PostFeedsRequestBodyNotRedacted(t *testing.T) {
	// Off-schema endpoint: even if the caller stuffs PII into a feed body,
	// the proxy is not in the business of heuristic redaction; the body
	// is persisted as-is. This is the explicit scope boundary in the spec.
	logger, ms, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	body := `{"feedType":"POST_PRODUCT_DATA","marketplaceIds":["A1PA6795UKMFR9"]}`
	req := httptest.NewRequest("POST", "/feeds/2021-06-30/feeds", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	// Body present verbatim. (Garbage-in is caller responsibility.)
	assert.Contains(t, string(allBodies[0].RequestBody), "POST_PRODUCT_DATA")

	// Metadata flag must be false: nothing was redacted, and downstream
	// consumers must not be misled into thinking the body is sanitized.
	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.False(t, allMeta[0].PIIRedactedRequest,
		"off-schema POST must NOT set PIIRedactedRequest")
}

func TestLoggingMiddleware_PostMfnRequestBody_OnlyShipFromAddressRedacted(t *testing.T) {
	// MFN createShipment: schema-verified that the ShipFromAddress field
	// holds the buyer's address in the return-shipment use case. Buyer
	// fields must be redacted; the AmazonOrderId stays untouched (Order IDs
	// are not direct PII per Amazon's DPP definition).
	logger, ms, bs := setupTestLogger(t)
	registry := pii.NewRegistry()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	body := `{
		"ShipmentRequestDetails": {
			"AmazonOrderId": "903-3489051-5871062",
			"ShipFromAddress": {
				"Name": "Real Buyer",
				"AddressLine1": "300 Turnbull Ave",
				"Email": "buyer@example.com",
				"Phone": "7132341234"
			}
		}
	}`
	req := httptest.NewRequest("POST", "/mfn/v0/shipments", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	rb := string(allBodies[0].RequestBody)
	assert.NotContains(t, rb, "Real Buyer")
	assert.NotContains(t, rb, "buyer@example.com")
	assert.NotContains(t, rb, "7132341234")
	assert.NotContains(t, rb, "300 Turnbull Ave")
	assert.Contains(t, rb, "903-3489051-5871062", "Amazon Order IDs are not direct PII; must remain")

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.True(t, allMeta[0].PIIRedactedRequest,
		"MFN POST must set PIIRedactedRequest=true in metadata")
}

func TestLoggingMiddleware_FailClosed_UnknownPostBody_FallsBackToFullBodyPlaceholder(t *testing.T) {
	// Reviewer-flagged Critical: in fail-closed mode, a POST to an
	// unrecognized SP-API path triggers RequestBodyContainsPII=true with
	// RequestBodyPattern returning the raw (unclassified) path. That path
	// has no rules registered, so the engine returns (body, false). Without
	// a full-body fallback, PIIRedactedRequest=true would be set on
	// metadata while the body lands on disk verbatim -- contradicting the
	// metadata flag and defeating fail-closed mode.
	//
	// Important: this test cannot reuse setupTestLogger because that helper
	// builds the AsyncLogger with a separate non-fail-closed Registry. In
	// production main.go shares a single Registry between the middleware
	// and the engine; we mirror that wiring here so the engine-side
	// IsFullBodyPII check sees the fail-closed flag.
	registry := pii.NewRegistryWithExtras(nil)
	registry.SetFailClosed(true)

	ms := &mockStore{}
	bs := &mockBodyStore{}
	logger := NewAsyncLogger(ms, bs, pii.NewEngine(registry), 100)
	t.Cleanup(func() { logger.Close() })

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})
	mw := LoggingMiddleware(logger, registry, "eu", 0)(handler)

	body := `{"someField":"victim@example.com","other":"sensitive data"}`
	req := httptest.NewRequest("POST", "/futureapi/2030-01-01/unknown-action", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	req = withMerchant(req, "merchant-a")

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	logger.Close()

	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	rb := string(allBodies[0].RequestBody)
	assert.NotContains(t, rb, "victim@example.com",
		"fail-closed unknown POST must redact body to placeholder")
	assert.NotContains(t, rb, "sensitive data")
	assert.Contains(t, rb, "redacted",
		"fail-closed fallback uses RedactFullBody, which produces a placeholder document")

	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.True(t, allMeta[0].PIIRedactedRequest,
		"fail-closed unknown POST must set PIIRedactedRequest=true and the body must reflect that")
}

// ── decompressIfGzipBounded (F-09) ────────────────────────────────────────

func TestDecompressIfGzipBounded_BombDefenseFallsBackToCompressed(t *testing.T) {
	// A 1 KiB cap, fed a gzip stream that decompresses to 10x the cap.
	// The function must NOT return the bomb's expanded contents; it falls
	// back to the original compressed bytes.
	const cap = 1024
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(bytes.Repeat([]byte{'A'}, 10*cap))
	_ = gw.Close()
	compressed := buf.Bytes()

	headers := http.Header{}
	headers.Set("Content-Encoding", "gzip")

	got := decompressIfGzipBounded(compressed, headers, cap)
	assert.Equal(t, compressed, got,
		"on overflow, fall back to compressed bytes rather than partially decompressed output")
}

func TestDecompressIfGzipBounded_NormalGzipWithinCap(t *testing.T) {
	// Sanity check: when the decompressed output fits, it is returned
	// fully. This ensures the bomb defense does not break normal traffic.
	const cap = 4096
	payload := []byte(`{"data":"hello"}`)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(payload)
	_ = gw.Close()

	headers := http.Header{}
	headers.Set("Content-Encoding", "gzip")

	got := decompressIfGzipBounded(buf.Bytes(), headers, cap)
	assert.Equal(t, payload, got)
}

func TestDecompressIfGzip_NotGzip_PassesThrough(t *testing.T) {
	// Pre-existing behavior preserved: non-gzip body is returned untouched.
	headers := http.Header{}
	body := []byte(`{"plain":"json"}`)
	assert.Equal(t, body, decompressIfGzip(body, headers))
}

func TestDecompressIfGzip_EmptyBody_ReturnsEmpty(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Encoding", "gzip")
	assert.Empty(t, decompressIfGzip(nil, headers))
}
