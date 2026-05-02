package logging_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spiohq/smart-proxy/internal/logging"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/tokenstore"
)

func TestLoggingMiddleware_SetsTokenInStore(t *testing.T) {
	ts := tokenstore.New()
	asyncLogger := logging.NewAsyncLoggerWithTokenStore(nil, nil, nil, 100, ts)

	mw := logging.LoggingMiddleware(asyncLogger, pii.NewRegistry(), "eu", 0)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|test-token-123")
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: "SELLER_A", Source: "header"})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	tok, ok := ts.Get("SELLER_A")
	if !ok {
		t.Fatal("expected token to be stored for SELLER_A")
	}
	if tok != "Atza|test-token-123" {
		t.Fatalf("got token %q, want %q", tok, "Atza|test-token-123")
	}
}

func TestLoggingMiddleware_EmptyToken_DoesNotStore(t *testing.T) {
	ts := tokenstore.New()
	asyncLogger := logging.NewAsyncLoggerWithTokenStore(nil, nil, nil, 100, ts)

	mw := logging.LoggingMiddleware(asyncLogger, pii.NewRegistry(), "eu", 0)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	// No X-Amz-Access-Token header
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: "SELLER_B", Source: "header"})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	_, ok := ts.Get("SELLER_B")
	if ok {
		t.Fatal("expected no token to be stored when header is absent")
	}
}

func TestLoggingMiddleware_NoMerchantKey_DoesNotStore(t *testing.T) {
	ts := tokenstore.New()
	asyncLogger := logging.NewAsyncLoggerWithTokenStore(nil, nil, nil, 100, ts)

	mw := logging.LoggingMiddleware(asyncLogger, pii.NewRegistry(), "eu", 0)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|some-token")
	// No merchant in context -- zero-value ResolvedMerchant has empty Key
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Token should NOT be stored because merchant key is empty
	// (we can't look it up without a key, so we try an arbitrary key)
	_, ok := ts.Get("")
	if ok {
		t.Fatal("expected no token stored for empty merchant key")
	}
}
