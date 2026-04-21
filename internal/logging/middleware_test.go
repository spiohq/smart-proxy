package logging

import (
	"bytes"
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
	assert.True(t, allMeta[0].PIIRedacted)
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
