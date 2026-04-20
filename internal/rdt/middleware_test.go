package rdt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/stretchr/testify/assert"
)

// newTestMiddleware creates a Middleware wired to a mock Tokens API server.
// Returns the middleware and a function to shut down the mock server.
func newTestMiddleware(t *testing.T, handler http.Handler) (*Middleware, func()) {
	t.Helper()
	tokensSrv := httptest.NewServer(handler)
	mw := NewMiddleware(
		NewCache(5*time.Minute),
		NewMinter(tokensSrv.URL, tokensSrv.Client()),
		NewReportTracker(10*time.Minute),
	)
	return mw, tokensSrv.Close
}

// wrapWithMerchant wraps a handler to inject merchant context, simulating the
// merchant resolver middleware that runs before us in the chain.
func wrapWithMerchant(merchantID string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := merchant.ContextWithMerchant(r.Context(), merchant.ResolvedMerchant{
			Key:    merchantID,
			Source: "header",
		})
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestMiddleware_FeatureOff_PassesThrough(t *testing.T) {
	// Middleware with nil minter (feature off)
	mw := NewMiddleware(NewCache(5*time.Minute), nil, nil)

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atza|my-lwa-token", receivedToken, "should pass through original token when feature is off")
}

func TestMiddleware_NonPIIEndpoint_PassesThrough(t *testing.T) {
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("tokens API should not be called for non-PII endpoints")
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/catalog/2022-04-01/items/B07XYZ", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atza|my-lwa-token", receivedToken)
}

func TestMiddleware_AlreadyRDT_PassesThrough(t *testing.T) {
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("tokens API should not be called when client already sends an RDT")
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atz.sprdt|already-have-rdt")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atz.sprdt|already-have-rdt", receivedToken)
}

func TestMiddleware_CacheMiss_MintsAndSwaps(t *testing.T) {
	var mintCalls atomic.Int32

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|fresh-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atz.sprdt|fresh-rdt", receivedToken, "should swap LWA token for minted RDT")
	assert.Equal(t, int32(1), mintCalls.Load())
}

func TestMiddleware_CacheHit_NoMint(t *testing.T) {
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("tokens API should not be called on cache hit")
	}))
	defer cleanup()

	// Pre-populate cache
	op := PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
	}
	key := BuildCacheKey("merchant-1", op)
	mw.cache.Set(key, CacheEntry{
		Token:     "Atz.sprdt|cached-rdt",
		ExpiresAt: time.Now().Add(55 * time.Minute),
	})

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/orders/v0/orders/999-888", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atz.sprdt|cached-rdt", receivedToken)
}

func TestMiddleware_403_InvalidatesCache(t *testing.T) {
	var mintCalls atomic.Int32

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|new-rdt-after-invalidation",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	// Pre-populate cache with a token that will cause 403
	op := PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
	}
	key := BuildCacheKey("merchant-1", op)
	mw.cache.Set(key, CacheEntry{
		Token:     "Atz.sprdt|revoked-rdt",
		ExpiresAt: time.Now().Add(55 * time.Minute),
	})

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Response should be the 403 from upstream
	assert.Equal(t, http.StatusForbidden, rr.Code)
	// Cache entry should be invalidated
	_, ok := mw.cache.Get(key)
	assert.False(t, ok, "cache entry should be invalidated after 403")
	// Should NOT have tried to mint (just invalidate and pass through)
	assert.Equal(t, int32(0), mintCalls.Load())
}

func TestMiddleware_MintError_FailOpen(t *testing.T) {
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":[{"code":"InternalError"}]}`))
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atza|my-lwa-token", receivedToken, "should fail open with original token")
}

func TestMiddleware_Singleflight_DeduplicatesConcurrentMints(t *testing.T) {
	var mintCalls atomic.Int32
	// Add a small delay to ensure both requests overlap
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|shared-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	var wg sync.WaitGroup
	tokens := make([]string, 2)

	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/orders/v0/orders/order-"+string(rune('A'+idx)), nil)
			req.Header.Set("x-amz-access-token", "Atza|same-lwa-token")
			rr := httptest.NewRecorder()

			// We need to inject merchant context in the goroutine
			ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{
				Key:    "merchant-1",
				Source: "header",
			})
			handler.ServeHTTP(rr, req.WithContext(ctx))
		}(i)
	}

	wg.Wait()

	// Only ONE upstream mint call should have been made
	assert.Equal(t, int32(1), mintCalls.Load(), "singleflight should deduplicate concurrent mints")
	_ = tokens // both should get the same RDT
}

func TestMiddleware_DifferentMerchants_SeparateMints(t *testing.T) {
	var mintCalls atomic.Int32

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|rdt-" + r.Header.Get("x-amz-access-token"),
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Request 1: merchant-1
	handler1 := wrapWithMerchant("merchant-1", mw.Handler(backend))
	req1 := httptest.NewRequest("GET", "/orders/v0/orders/123", nil)
	req1.Header.Set("x-amz-access-token", "Atza|token-merchant-1")
	rr1 := httptest.NewRecorder()
	handler1.ServeHTTP(rr1, req1)

	// Request 2: merchant-2
	handler2 := wrapWithMerchant("merchant-2", mw.Handler(backend))
	req2 := httptest.NewRequest("GET", "/orders/v0/orders/456", nil)
	req2.Header.Set("x-amz-access-token", "Atza|token-merchant-2")
	rr2 := httptest.NewRecorder()
	handler2.ServeHTTP(rr2, req2)

	assert.Equal(t, int32(2), mintCalls.Load(), "different merchants should trigger separate mints")
}

func TestMiddleware_Report_FullSniffFlow_RestrictedType(t *testing.T) {
	// The 3-step flow:
	// 1. POST /reports -> sniff reportType from request body, reportId from response
	// 2. GET /reports/{reportId} -> sniff reportDocumentId from response
	// 3. GET /documents/{docId} -> mint RDT with concrete path

	var mintCalls atomic.Int32
	var mintedPath string

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		var body CreateRDTRequest
		json.NewDecoder(r.Body).Decode(&body)
		mintedPath = body.RestrictedResources[0].Path

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|report-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/reports/2021-06-30/reports":
			// Step 1: return reportId
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reportId":"report-ABC"}`))

		case r.Method == "GET" && r.URL.Path == "/reports/2021-06-30/reports/report-ABC":
			// Step 2: return report with documentId
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reportId":"report-ABC","reportType":"GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING","reportDocumentId":"doc-XYZ","processingStatus":"DONE"}`))

		case r.Method == "GET" && r.URL.Path == "/reports/2021-06-30/documents/doc-XYZ":
			// Step 3: document download
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reportDocumentId":"doc-XYZ","url":"https://s3.amazonaws.com/..."}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// Step 1: POST /reports with restricted reportType
	req1 := httptest.NewRequest("POST", "/reports/2021-06-30/reports", strings.NewReader(
		`{"reportType":"GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING","marketplaceIds":["ATVPDKIKX0DER"]}`,
	))
	req1.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	assert.Equal(t, http.StatusOK, rr1.Code)
	assert.Equal(t, int32(0), mintCalls.Load(), "no mint on POST /reports")

	// Step 2: GET /reports/{reportId}
	req2 := httptest.NewRequest("GET", "/reports/2021-06-30/reports/report-ABC", nil)
	req2.Header.Set("x-amz-access-token", "Atza|lwa-token")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	assert.Equal(t, http.StatusOK, rr2.Code)
	assert.Equal(t, int32(0), mintCalls.Load(), "no mint on GET /reports/{id}")

	// Step 3: GET /documents/{docId} -> should mint with concrete path
	req3 := httptest.NewRequest("GET", "/reports/2021-06-30/documents/doc-XYZ", nil)
	req3.Header.Set("x-amz-access-token", "Atza|lwa-token")
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)
	assert.Equal(t, http.StatusOK, rr3.Code)
	assert.Equal(t, int32(1), mintCalls.Load(), "should mint for restricted report document")
	assert.Equal(t, "/reports/2021-06-30/documents/doc-XYZ", mintedPath, "must use concrete path, not generic")
}

func TestMiddleware_Report_NonRestrictedType_NoMint(t *testing.T) {
	var mintCalls atomic.Int32

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|should-not-happen",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/reports/2021-06-30/reports":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reportId":"report-NR"}`))

		case r.Method == "GET" && r.URL.Path == "/reports/2021-06-30/reports/report-NR":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reportId":"report-NR","reportType":"GET_FLAT_FILE_OPEN_LISTINGS_DATA","reportDocumentId":"doc-NR","processingStatus":"DONE"}`))

		case r.Method == "GET" && r.URL.Path == "/reports/2021-06-30/documents/doc-NR":
			receivedToken = r.Header.Get("x-amz-access-token")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reportDocumentId":"doc-NR","url":"https://s3.amazonaws.com/..."}`))
		}
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// Step 1: POST /reports with non-restricted type
	req1 := httptest.NewRequest("POST", "/reports/2021-06-30/reports", strings.NewReader(
		`{"reportType":"GET_FLAT_FILE_OPEN_LISTINGS_DATA","marketplaceIds":["ATVPDKIKX0DER"]}`,
	))
	req1.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req1.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Step 2: GET /reports/{reportId}
	req2 := httptest.NewRequest("GET", "/reports/2021-06-30/reports/report-NR", nil)
	req2.Header.Set("x-amz-access-token", "Atza|lwa-token")
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	// Step 3: GET /documents/{docId} -> should NOT mint
	req3 := httptest.NewRequest("GET", "/reports/2021-06-30/documents/doc-NR", nil)
	req3.Header.Set("x-amz-access-token", "Atza|lwa-token")
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, req3)

	assert.Equal(t, int32(0), mintCalls.Load(), "should NOT mint for non-restricted report type")
	assert.Equal(t, "Atza|lwa-token", receivedToken, "should pass through original token")
}

func TestMiddleware_Report_UnknownDocumentId_PassesThrough(t *testing.T) {
	// Proxy missed step 1+2, client jumps directly to document download.
	var mintCalls atomic.Int32

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|should-not-happen",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// Direct document download without prior POST/GET steps
	req := httptest.NewRequest("GET", "/reports/2021-06-30/documents/unknown-doc-id", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, int32(0), mintCalls.Load(), "should NOT mint for unknown document")
	assert.Equal(t, "Atza|lwa-token", receivedToken, "should pass through original token")
}

func TestMiddleware_Report_AlreadyRDT_PassesThrough(t *testing.T) {
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not mint when client already sends RDT")
	}))
	defer cleanup()

	// Pre-populate tracker so we know it's restricted
	mw.reports.TrackReportCreation("report-R", "GET_VAT_TRANSACTION_DATA")
	mw.reports.TrackReportDocument("report-R", "doc-R")

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	req := httptest.NewRequest("GET", "/reports/2021-06-30/documents/doc-R", nil)
	req.Header.Set("x-amz-access-token", "Atz.sprdt|client-already-has-rdt")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atz.sprdt|client-already-has-rdt", receivedToken)
}

func TestMiddleware_ForceRDT_True_MintsForNonPIIEndpoint(t *testing.T) {
	// X-SP-Proxy-Force-RDT: true should force minting even for non-PII endpoints
	var mintCalls atomic.Int32
	var mintedPath string

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		var body CreateRDTRequest
		json.NewDecoder(r.Body).Decode(&body)
		mintedPath = body.RestrictedResources[0].Path

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|forced-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// Non-PII endpoint, but with force header
	req := httptest.NewRequest("GET", "/catalog/2022-04-01/items/B07XYZ", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, int32(1), mintCalls.Load(), "should mint when force header is true")
	assert.Equal(t, "Atz.sprdt|forced-rdt", receivedToken)
	assert.Equal(t, "/catalog/2022-04-01/items/B07XYZ", mintedPath, "should use concrete request path")
}

func TestMiddleware_ForceRDT_True_ReportDocumentWithoutSniffing(t *testing.T) {
	// Client gets reportDocumentId from REPORT_PROCESSING_FINISHED notification
	// and jumps directly to document download with force header.
	var mintCalls atomic.Int32
	var mintedPath string

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		var body CreateRDTRequest
		json.NewDecoder(r.Body).Decode(&body)
		mintedPath = body.RestrictedResources[0].Path

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|forced-report-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// Direct document download with force header (no prior POST/GET sniffing)
	req := httptest.NewRequest("GET", "/reports/2021-06-30/documents/amzn1.spdoc.1.4.na.from-notification", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, int32(1), mintCalls.Load())
	assert.Equal(t, "Atz.sprdt|forced-report-rdt", receivedToken)
	assert.Equal(t, "/reports/2021-06-30/documents/amzn1.spdoc.1.4.na.from-notification", mintedPath)
}

func TestMiddleware_ForceRDT_False_SkipsAutoMintOnPIIEndpoint(t *testing.T) {
	// X-SP-Proxy-Force-RDT: false should skip minting even for PII endpoints
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not mint when force header is false")
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// PII endpoint, but with force=false
	req := httptest.NewRequest("GET", "/orders/v0/orders/123-456", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Force-RDT", "false")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atza|lwa-token", receivedToken, "should pass through original token when force=false")
}

func TestMiddleware_ForceRDT_True_AlreadyRDT_PassesThrough(t *testing.T) {
	// Even with force=true, if token is already an RDT, don't mint again
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not mint when token is already an RDT")
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	req := httptest.NewRequest("GET", "/some/endpoint", nil)
	req.Header.Set("x-amz-access-token", "Atz.sprdt|already-rdt")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atz.sprdt|already-rdt", receivedToken)
}

func TestMiddleware_ForceRDT_HeaderStrippedBeforeUpstream(t *testing.T) {
	// The force header should not be forwarded to Amazon
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var upstreamForceHeader string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamForceHeader = r.Header.Get("X-SP-Proxy-Force-RDT")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	req := httptest.NewRequest("GET", "/orders/v0/orders/123", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, upstreamForceHeader, "force header must be stripped before forwarding to upstream")
}

func TestMiddleware_ForceRDT_MintFailure_FailOpen(t *testing.T) {
	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer cleanup()

	var receivedToken string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("x-amz-access-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	req := httptest.NewRequest("GET", "/reports/2021-06-30/documents/doc-from-notification", nil)
	req.Header.Set("x-amz-access-token", "Atza|lwa-token")
	req.Header.Set("X-SP-Proxy-Force-RDT", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Atza|lwa-token", receivedToken, "should fail open with original token")
}

func TestMiddleware_SecondRequest_UsesCachedRDT(t *testing.T) {
	var mintCalls atomic.Int32

	mw, cleanup := newTestMiddleware(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateRDTResponse{
			RestrictedDataToken: "Atz.sprdt|cached-rdt",
			ExpiresIn:           3600,
		})
	}))
	defer cleanup()

	var receivedTokens []string
	var mu sync.Mutex
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedTokens = append(receivedTokens, r.Header.Get("x-amz-access-token"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	handler := wrapWithMerchant("merchant-1", mw.Handler(backend))

	// First request - cache miss, mints
	req1 := httptest.NewRequest("GET", "/orders/v0/orders/order-AAA", nil)
	req1.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	// Second request - different order ID, same operation - cache hit
	req2 := httptest.NewRequest("GET", "/orders/v0/orders/order-BBB", nil)
	req2.Header.Set("x-amz-access-token", "Atza|my-lwa-token")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, int32(1), mintCalls.Load(), "second request should use cached RDT")
	assert.Equal(t, []string{"Atz.sprdt|cached-rdt", "Atz.sprdt|cached-rdt"}, receivedTokens)
	_ = rr1
	_ = rr2
}
