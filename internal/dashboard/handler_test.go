package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock stores ---

type mockLogStore struct {
	logs      []*storage.RequestLog
	total     int64
	merchants []string
}

func (m *mockLogStore) LogRequest(ctx context.Context, entry *storage.RequestLog) error { return nil }
func (m *mockLogStore) LogRequestBatch(ctx context.Context, entries []*storage.RequestLog) error {
	return nil
}
func (m *mockLogStore) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockLogStore) NullifyBodyRefs(ctx context.Context, files []string) (int64, error) {
	return 0, nil
}
func (m *mockLogStore) QueryByTimeRange(ctx context.Context, from, to time.Time) ([]*storage.RequestLog, error) {
	return nil, nil
}
func (m *mockLogStore) QueryLogs(ctx context.Context, filter storage.LogFilter) ([]*storage.RequestLog, int64, error) {
	return m.logs, m.total, nil
}
func (m *mockLogStore) QueryByID(ctx context.Context, id string) (*storage.RequestLog, error) {
	for _, l := range m.logs {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, nil
}
func (m *mockLogStore) Maintain(ctx context.Context) error { return nil }
func (m *mockLogStore) Migrate(ctx context.Context) error  { return nil }
func (m *mockLogStore) Close() error                       { return nil }
func (m *mockLogStore) DistinctMerchants(ctx context.Context, prefix string, limit int) ([]string, error) {
	if m.merchants == nil {
		return []string{}, nil
	}
	var result []string
	for _, mk := range m.merchants {
		if prefix == "" || strings.HasPrefix(mk, prefix) {
			result = append(result, mk)
		}
	}
	return result, nil
}
func (m *mockLogStore) MethodsByEndpoint(ctx context.Context, from, to time.Time) (map[string][]string, error) {
	return nil, nil
}

type mockAuditStore struct {
	events []*audit.AuditEvent
	total  int64
}

func (m *mockAuditStore) LogAuditEvent(ctx context.Context, event *audit.AuditEvent) error {
	return nil
}
func (m *mockAuditStore) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockAuditStore) QueryAuditEvents(ctx context.Context, filter audit.AuditFilter) ([]*audit.AuditEvent, int64, error) {
	return m.events, m.total, nil
}
func (m *mockAuditStore) Close() error { return nil }

type mockBodyStore struct{}

func (m *mockBodyStore) Write(ctx context.Context, entry *bodies.BodyEntry) (string, int64, int, error) {
	return "", 0, 0, nil
}
func (m *mockBodyStore) Read(ctx context.Context, file string, offset int64, length int) (*bodies.BodyEntry, error) {
	return &bodies.BodyEntry{
		ID:           "test",
		RequestBody:  json.RawMessage(`{"req":"data"}`),
		ResponseBody: json.RawMessage(`{"res":"data"}`),
	}, nil
}
func (m *mockBodyStore) Close() error { return nil }

// mockBodyStoreMap is a body store whose Read method returns entries keyed by
// filename. Used by tests that need to control the body content per log entry.
type mockBodyStoreMap struct {
	entries map[string]*bodies.BodyEntry
}

func (m *mockBodyStoreMap) Write(ctx context.Context, entry *bodies.BodyEntry) (string, int64, int, error) {
	return "", 0, 0, nil
}
func (m *mockBodyStoreMap) Read(ctx context.Context, file string, offset int64, length int) (*bodies.BodyEntry, error) {
	if e, ok := m.entries[file]; ok {
		return e, nil
	}
	return &bodies.BodyEntry{ID: "missing"}, nil
}
func (m *mockBodyStoreMap) Close() error { return nil }

// --- Tests ---

func TestHandleLogs(t *testing.T) {
	now := time.Now().UTC()
	ls := &mockLogStore{
		logs: []*storage.RequestLog{
			{ID: "req-1", Timestamp: now, MerchantKey: "m", Method: "GET", Path: "/test", StatusCode: 200,
				TotalLatencyMs: 50, CacheStatus: "MISS"},
		},
		total: 1,
	}

	h := NewHandler(ls, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["total"])
	rows := resp["rows"].([]any)
	assert.Len(t, rows, 1)
}

func TestHandleLogByID(t *testing.T) {
	ls := &mockLogStore{
		logs: []*storage.RequestLog{
			{ID: "req-abc", MerchantKey: "m", Method: "GET", Path: "/test", StatusCode: 200,
				BodyFile: "test.jsonl", BodyOffset: 100, BodyLength: 50,
				RequestHeaders:  map[string]string{"Auth": "[REDACTED]"},
				ResponseHeaders: map[string]string{"Content-Type": "application/json"}},
		},
	}

	h := NewHandler(ls, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/req-abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "req-abc", resp["id"])
	assert.Equal(t, true, resp["hasBody"])
}

func TestHandleLogByID_NotFound(t *testing.T) {
	h := NewHandler(&mockLogStore{}, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleLogBody(t *testing.T) {
	ls := &mockLogStore{
		logs: []*storage.RequestLog{
			{ID: "req-body", BodyFile: "test.jsonl", BodyOffset: 100, BodyLength: 50},
		},
	}

	h := NewHandler(ls, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/req-body/body", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["requestBody"])
	assert.NotNil(t, resp["responseBody"])
}

func TestHandleLogBody_NoBody(t *testing.T) {
	ls := &mockLogStore{
		logs: []*storage.RequestLog{
			{ID: "req-nobody", BodyFile: ""},
		},
	}

	h := NewHandler(ls, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/req-nobody/body", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAudit(t *testing.T) {
	now := time.Now().UTC()
	as := &mockAuditStore{
		events: []*audit.AuditEvent{
			{ID: "evt-1", Timestamp: now, EventType: "startup", Source: "main", Message: "started"},
			{ID: "evt-2", Timestamp: now, EventType: "purge", Source: "scheduler", Message: "purged 10 rows"},
		},
		total: 2,
	}

	h := NewHandler(&mockLogStore{}, as, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(2), resp["total"])
	rows := resp["rows"].([]any)
	assert.Len(t, rows, 2)
}

func TestHandleAudit_WithEventTypeFilter(t *testing.T) {
	as := &mockAuditStore{events: []*audit.AuditEvent{}, total: 0}

	h := NewHandler(&mockLogStore{}, as, &mockBodyStore{})
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/audit?eventType=purge", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestIntegration_APIRoundTrip(t *testing.T) {
	// Use real SQLite stores
	metaStore, err := storage.NewSQLiteStore(":memory:")
	require.NoError(t, err)
	defer metaStore.Close()

	auditSt := audit.NewSQLiteStore(metaStore.DB())

	ctx := context.Background()
	now := time.Now().UTC()

	// Insert test data
	require.NoError(t, metaStore.LogRequest(ctx, &storage.RequestLog{
		ID: "int-001", Timestamp: now, MerchantKey: "merchant-a",
		Region: "eu", Method: "GET", Path: "/orders/v0/orders",
		StatusCode: 200, CacheStatus: "MISS", TotalLatencyMs: 50,
	}))

	require.NoError(t, auditSt.LogAuditEvent(ctx, &audit.AuditEvent{
		ID: "audit-int-001", Timestamp: now, EventType: "startup",
		Source: "test", Message: "integration test",
	}))

	h := NewHandler(metaStore, auditSt, &mockBodyStore{})
	mux := NewMux(h)

	// Test logs
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/logs", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	var logsResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &logsResp)
	assert.Equal(t, float64(1), logsResp["total"])

	// Test log by ID
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/logs/int-001", nil))
	assert.Equal(t, http.StatusOK, w.Code)

	// Test audit
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/audit", nil))
	assert.Equal(t, http.StatusOK, w.Code)

	// Test health (still works)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/_sp-proxy/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)

	// Test SPA fallback
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/requests", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Smart Proxy by Spio")

	// Test merchants autocomplete
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/merchants", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	var merchantsResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &merchantsResp)
	assert.NotNil(t, merchantsResp["merchants"])
}

func TestHandleMerchants(t *testing.T) {
	ls := &mockLogStore{
		merchants: []string{"SELLER_ABC123", "SELLER_XYZ789", "AGENCY_DEMO"},
	}
	h := NewHandler(ls, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	// No filter  -  returns all
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/merchants", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string][]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["merchants"], 3)

	// With prefix filter
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/merchants?q=SELLER", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["merchants"], 2)
	assert.Contains(t, resp["merchants"], "SELLER_ABC123")
	assert.Contains(t, resp["merchants"], "SELLER_XYZ789")

	// With prefix filter  -  no match
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/merchants?q=NONEXISTENT", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp["merchants"])
}

func TestHandleMerchants_Empty(t *testing.T) {
	h := NewHandler(&mockLogStore{}, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/merchants", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["merchants"], "merchants must be empty array, not null")
	assert.Empty(t, resp["merchants"])
}

func TestSPA_AllReferencedAssetsExist(t *testing.T) {
	h := NewHandler(&mockLogStore{}, &mockAuditStore{}, &mockBodyStore{})
	mux := NewMux(h)

	// Fetch index.html
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	require.Equal(t, http.StatusOK, w.Code)

	html := w.Body.String()
	require.Contains(t, html, "Smart Proxy by Spio", "index.html must contain app title")

	// Extract all referenced asset paths from HTML (href="..." and src="...")
	var assetPaths []string
	for _, attr := range []string{`href="`, `src="`} {
		remaining := html
		for {
			idx := strings.Index(remaining, attr)
			if idx < 0 {
				break
			}
			remaining = remaining[idx+len(attr):]
			end := strings.Index(remaining, `"`)
			if end < 0 {
				break
			}
			path := remaining[:end]
			remaining = remaining[end:]

			// Only check local asset paths (skip external URLs and data URIs)
			if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
				assetPaths = append(assetPaths, path)
			}
		}
	}

	require.NotEmpty(t, assetPaths, "index.html must reference at least one asset")

	for _, path := range assetPaths {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))

		contentType := w.Header().Get("Content-Type")

		if strings.HasSuffix(path, ".js") {
			assert.Equal(t, http.StatusOK, w.Code, "asset %s must return 200", path)
			assert.Contains(t, contentType, "javascript",
				"asset %s must be served as JavaScript, got %s (SPA fallback serving HTML instead of asset)", path, contentType)
		} else if strings.HasSuffix(path, ".css") {
			assert.Equal(t, http.StatusOK, w.Code, "asset %s must return 200", path)
			assert.Contains(t, contentType, "css",
				"asset %s must be served as CSS, got %s", path, contentType)
		} else {
			assert.Equal(t, http.StatusOK, w.Code, "asset %s must return 200", path)
		}
	}
}

func TestHandleLogBody_ReadSideRedaction_LegacyMfnRequestBody(t *testing.T) {
	// Simulate a JSONL entry written before F-02 closed the write-side leak:
	// MFN createShipment POST with unredacted ShipFromAddress (the buyer).
	// The read-side filter must redact on serve.
	ls := &mockLogStore{
		logs: []*storage.RequestLog{{
			ID:                  "legacy-mfn-001",
			Timestamp:           time.Now().UTC(),
			Method:              "POST",
			Path:                "/mfn/v0/shipments",
			StatusCode:          200,
			PIIRedactedRequest:  false, // legacy: write-side did not redact
			PIIRedactedResponse: false,
			BodyFile:            "legacy.jsonl",
			BodyOffset:          0,
			BodyLength:          400,
		}},
	}
	bs := &mockBodyStoreMap{
		entries: map[string]*bodies.BodyEntry{
			"legacy.jsonl": {
				ID:           "legacy-mfn-001",
				RequestBody:  json.RawMessage(`{"ShipmentRequestDetails":{"AmazonOrderId":"903-3489051-5871062","ShipFromAddress":{"Name":"Real Buyer","Email":"buyer@example.com","AddressLine1":"300 Turnbull Ave"}}}`),
				ResponseBody: json.RawMessage(`{"payload":{"ShipmentId":"S1"}}`),
			},
		},
	}

	engine := pii.NewEngine(pii.NewRegistry())
	h := NewHandlerWithPII(ls, &mockAuditStore{}, bs, engine)
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/legacy-mfn-001/body", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	respBody := rec.Body.String()
	assert.NotContains(t, respBody, "Real Buyer", "F-15: read-side filter must redact legacy MFN buyer name")
	assert.NotContains(t, respBody, "buyer@example.com")
	assert.NotContains(t, respBody, "300 Turnbull Ave")
	assert.Contains(t, respBody, "903-3489051-5871062", "Order IDs are not direct PII; preserved")
}

func TestHandleLogBody_ReadSideRedaction_LegacyMessagingRequestBody(t *testing.T) {
	ls := &mockLogStore{
		logs: []*storage.RequestLog{{
			ID:                  "legacy-msg-001",
			Timestamp:           time.Now().UTC(),
			Method:              "POST",
			Path:                "/messaging/v1/orders/903-3489051-5871062/messages/createConfirmServiceDetails",
			StatusCode:          200,
			PIIRedactedRequest:  false,
			PIIRedactedResponse: false,
			BodyFile:            "legacy-msg.jsonl",
			BodyOffset:          0,
			BodyLength:          200,
		}},
	}
	bs := &mockBodyStoreMap{
		entries: map[string]*bodies.BodyEntry{
			"legacy-msg.jsonl": {
				ID:          "legacy-msg-001",
				RequestBody: json.RawMessage(`{"message":{"text":"Hi Maria, see you at Hauptstrasse 42."}}`),
			},
		},
	}

	engine := pii.NewEngine(pii.NewRegistry())
	h := NewHandlerWithPII(ls, &mockAuditStore{}, bs, engine)
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/legacy-msg-001/body", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	respBody := rec.Body.String()
	assert.NotContains(t, respBody, "Maria", "F-15: read-side filter must redact legacy messaging text")
	assert.NotContains(t, respBody, "Hauptstrasse")
}

func TestHandleLogBody_ReadSideRedaction_LegacyOrdersV0ResponseBody(t *testing.T) {
	// Response-side coverage. Symmetric to the request-side tests but exercises
	// the field-level RedactForLogging path (orders/v0/orders has rules
	// for $.payload.Orders[*].BuyerInfo.* and ShippingAddress.*).
	id := "legacy-orders-001"
	ls := &mockLogStore{
		logs: []*storage.RequestLog{{
			ID:         id,
			Timestamp:  time.Now().UTC(),
			Method:     "GET",
			Path:       "/orders/v0/orders",
			StatusCode: 200,
			BodyFile:   "legacy-orders.jsonl",
			BodyOffset: 0,
			BodyLength: 400,
		}},
	}
	bs := &mockBodyStoreMap{
		entries: map[string]*bodies.BodyEntry{
			"legacy-orders.jsonl": {
				ID: id,
				ResponseBody: json.RawMessage(`{"payload":{"Orders":[{"AmazonOrderId":"903-3489051-5871062","BuyerInfo":{"BuyerEmail":"buyer@example.com","BuyerName":"Maria Schmidt"},"ShippingAddress":{"Name":"Maria Schmidt","AddressLine1":"Hauptstrasse 42"}}]}}`),
			},
		},
	}

	engine := pii.NewEngine(pii.NewRegistry())
	h := NewHandlerWithPII(ls, &mockAuditStore{}, bs, engine)
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/"+id+"/body", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	respBody := rec.Body.String()
	assert.NotContains(t, respBody, "Maria Schmidt", "F-15: read-side filter must redact legacy response BuyerInfo / ShippingAddress")
	assert.NotContains(t, respBody, "buyer@example.com")
	assert.NotContains(t, respBody, "Hauptstrasse 42")
	assert.Contains(t, respBody, "903-3489051-5871062", "Order IDs are not direct PII; preserved")
}

func TestHandleLogBody_ReadSideRedaction_LegacyFullBodyPIIResponse(t *testing.T) {
	// IsFullBodyPII path: /orders/v0/orders/{orderId}/buyerInfo is in the
	// fullBodyEndpoints set, so a legacy response gets replaced with the
	// placeholder document instead of field-level redaction.
	id := "legacy-buyerinfo-001"
	ls := &mockLogStore{
		logs: []*storage.RequestLog{{
			ID:         id,
			Timestamp:  time.Now().UTC(),
			Method:     "GET",
			Path:       "/orders/v0/orders/903-3489051-5871062/buyerInfo",
			StatusCode: 200,
			BodyFile:   "legacy-buyerinfo.jsonl",
			BodyOffset: 0,
			BodyLength: 200,
		}},
	}
	bs := &mockBodyStoreMap{
		entries: map[string]*bodies.BodyEntry{
			"legacy-buyerinfo.jsonl": {
				ID:           id,
				ResponseBody: json.RawMessage(`{"payload":{"BuyerEmail":"buyer@example.com","BuyerName":"Maria Schmidt","PurchaseOrderNumber":"PO-12345"}}`),
			},
		},
	}

	engine := pii.NewEngine(pii.NewRegistry())
	h := NewHandlerWithPII(ls, &mockAuditStore{}, bs, engine)
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/"+id+"/body", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	respBody := rec.Body.String()
	assert.NotContains(t, respBody, "Maria Schmidt")
	assert.NotContains(t, respBody, "buyer@example.com")
	assert.NotContains(t, respBody, "PO-12345")
	assert.Contains(t, respBody, "redacted",
		"full-body PII endpoints replace the entire response with the placeholder")
}

func TestHandleLogBody_NoEngine_BodyReturnedVerbatim(t *testing.T) {
	ls := &mockLogStore{
		logs: []*storage.RequestLog{{
			ID:         "no-engine-001",
			Timestamp:  time.Now().UTC(),
			Method:     "GET",
			Path:       "/orders/v0/orders",
			StatusCode: 200,
			BodyFile:   "raw.jsonl",
			BodyOffset: 0,
			BodyLength: 100,
		}},
	}
	bs := &mockBodyStoreMap{
		entries: map[string]*bodies.BodyEntry{
			"raw.jsonl": {
				ID:           "no-engine-001",
				ResponseBody: json.RawMessage(`{"data":"unfiltered"}`),
			},
		},
	}

	// Plain NewHandler -- no engine wired. Existing test setups behave like this.
	h := NewHandler(ls, &mockAuditStore{}, bs)
	mux := NewMux(h)

	req := httptest.NewRequest("GET", "/api/v1/logs/no-engine-001/body", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "unfiltered",
		"NewHandler without engine must NOT alter body content")
}
