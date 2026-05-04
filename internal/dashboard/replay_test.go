package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/dashboard"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/spiohq/smart-proxy/internal/tokenstore"
)

func TestHandleReplay_NotFound(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()
	h := dashboard.NewHandlerWithPIIAndReplay(store, auditStore, nil, nil, ts)
	mux := dashboard.NewMux(h)

	req := httptest.NewRequest("POST", "/api/v1/logs/does-not-exist/replay", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

func TestHandleReplay_NoToken_ReturnsUnavailable(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()

	ctx := context.Background()
	logEntry := &storage.RequestLog{
		ID:          "replay-no-token-001",
		Timestamp:   time.Now().UTC(),
		MerchantKey: "SELLER_NO_TOKEN",
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

	req := httptest.NewRequest("POST", "/api/v1/logs/replay-no-token-001/replay", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var result map[string]any
	json.NewDecoder(rr.Body).Decode(&result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
	if result["reason"] == "" || result["reason"] == nil {
		t.Fatal("expected non-empty reason")
	}
}

func TestHandleReplay_WithToken_ForwardsRequest(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()
	ts.Set("SELLER_Y", "Atza|fresh-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Access-Token") != "Atza|fresh-token" {
			http.Error(w, "wrong token", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"orders":[]}`))
	}))
	defer upstream.Close()

	ctx := context.Background()
	logEntry := &storage.RequestLog{
		ID:          "replay-with-token-001",
		Timestamp:   time.Now().UTC(),
		MerchantKey: "SELLER_Y",
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
	h.SetProxyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamURL, _ := url.Parse(upstream.URL)
		r.URL.Host = upstreamURL.Host
		r.URL.Scheme = upstreamURL.Scheme
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		body := make([]byte, 4096)
		n, _ := resp.Body.Read(body)
		w.Write(body[:n])
	}))

	mux := dashboard.NewMux(h)
	req := httptest.NewRequest("POST", "/api/v1/logs/replay-with-token-001/replay", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var result map[string]any
	json.NewDecoder(rr.Body).Decode(&result)
	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}
	sc, _ := result["statusCode"].(float64)
	if int(sc) != 200 {
		t.Fatalf("expected statusCode=200, got %v", result["statusCode"])
	}
}

func TestHandleReplay_RoutesToCorrectRegionHandler(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auditStore := audit.NewSQLiteStore(store.DB())
	ts := tokenstore.New()
	ts.Set("SELLER_NA", "Atza|na-token")

	// EU upstream responds with a marker so we can detect which handler was used.
	euUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Region-Hit", "eu")
		w.WriteHeader(200)
		w.Write([]byte(`{"region":"eu"}`))
	}))
	defer euUpstream.Close()

	naUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Region-Hit", "na")
		w.WriteHeader(200)
		w.Write([]byte(`{"region":"na"}`))
	}))
	defer naUpstream.Close()

	makeForwardingHandler := func(upstreamURL string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := url.Parse(upstreamURL)
			r.URL.Host = u.Host
			r.URL.Scheme = u.Scheme
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer resp.Body.Close()
			for k, vals := range resp.Header {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			body := make([]byte, 4096)
			n, _ := resp.Body.Read(body)
			w.Write(body[:n])
		})
	}

	ctx := context.Background()
	// Log entry is from the NA region.
	logEntry := &storage.RequestLog{
		ID:          "replay-region-na-001",
		Timestamp:   time.Now().UTC(),
		MerchantKey: "SELLER_NA",
		Region:      "na",
		Method:      "GET",
		Path:        "/orders/v0/orders",
		StatusCode:  200,
		CacheStatus: "MISS",
	}
	if err := store.LogRequest(ctx, logEntry); err != nil {
		t.Fatal(err)
	}

	h := dashboard.NewHandlerWithPIIAndReplay(store, auditStore, nil, nil, ts)
	h.SetRegionHandlers(map[string]http.Handler{
		"eu": makeForwardingHandler(euUpstream.URL),
		"na": makeForwardingHandler(naUpstream.URL),
	})
	mux := dashboard.NewMux(h)

	req := httptest.NewRequest("POST", "/api/v1/logs/replay-region-na-001/replay", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	var result map[string]any
	json.NewDecoder(rr.Body).Decode(&result)
	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}
	// The response body must come from the NA upstream, not EU.
	body, _ := result["responseBody"].(map[string]any)
	if body["region"] != "na" {
		t.Fatalf("expected replay to hit NA handler, got responseBody=%v", result["responseBody"])
	}
}
