package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/dashboard"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/spiohq/smart-proxy/internal/tokenstore"
)

func TestLogDetail_ReplayAvailable_WhenTokenPresent(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()
	ts.Set("SELLER_WITH_TOKEN", "Atza|some-valid-token")

	ctx := context.Background()
	logEntry := &storage.RequestLog{
		ID:          "log-detail-replay-001",
		Timestamp:   time.Now().UTC(),
		MerchantKey: "SELLER_WITH_TOKEN",
		Region:      "eu",
		Method:      "GET",
		Path:        "/orders/v0/orders",
		StatusCode:  200,
		CacheStatus: "MISS",
	}
	if err := store.LogRequest(ctx, logEntry); err != nil {
		t.Fatal(err)
	}

	h := dashboard.NewHandlerWithPIIAndReplay(store, auditStore, nil, nil, ts)
	mux := dashboard.NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/log-detail-replay-001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var result map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["replayAvailable"] != true {
		t.Fatalf("expected replayAvailable=true, got %v", result["replayAvailable"])
	}
	if reason, ok := result["replayUnavailableReason"]; ok && reason != "" {
		t.Fatalf("expected replayUnavailableReason to be absent or empty, got %v", reason)
	}
}

func TestLogDetail_ReplayUnavailable_WhenPIIRedactedRequest(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()
	ts.Set("SELLER_WITH_TOKEN", "Atza|some-valid-token")

	ctx := context.Background()
	logEntry := &storage.RequestLog{
		ID:                 "log-detail-replay-pii-001",
		Timestamp:          time.Now().UTC(),
		MerchantKey:        "SELLER_WITH_TOKEN",
		Region:             "eu",
		Method:             "POST",
		Path:               "/products/fees/v0/feesEstimate",
		StatusCode:         200,
		CacheStatus:        "MISS",
		PIIRedactedRequest: true,
	}
	if err := store.LogRequest(ctx, logEntry); err != nil {
		t.Fatal(err)
	}

	h := dashboard.NewHandlerWithPIIAndReplay(store, auditStore, nil, nil, ts)
	mux := dashboard.NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/log-detail-replay-pii-001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var result map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["replayAvailable"] != false {
		t.Fatalf("expected replayAvailable=false, got %v", result["replayAvailable"])
	}
	reason, _ := result["replayUnavailableReason"].(string)
	if reason == "" {
		t.Fatal("expected non-empty replayUnavailableReason")
	}
}

func TestLogDetail_ReplayUnavailable_WhenNoToken(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()

	ctx := context.Background()
	logEntry := &storage.RequestLog{
		ID:          "log-detail-replay-002",
		Timestamp:   time.Now().UTC(),
		MerchantKey: "SELLER_NO_TOKEN_DETAIL",
		Region:      "eu",
		Method:      "GET",
		Path:        "/orders/v0/orders",
		StatusCode:  200,
		CacheStatus: "MISS",
	}
	if err := store.LogRequest(ctx, logEntry); err != nil {
		t.Fatal(err)
	}

	h := dashboard.NewHandlerWithPIIAndReplay(store, auditStore, nil, nil, ts)
	mux := dashboard.NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/log-detail-replay-002", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var result map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["replayAvailable"] != false {
		t.Fatalf("expected replayAvailable=false, got %v", result["replayAvailable"])
	}
	reason, _ := result["replayUnavailableReason"].(string)
	if reason == "" {
		t.Fatal("expected non-empty replayUnavailableReason")
	}
}
