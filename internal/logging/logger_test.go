package logging

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStore records calls to LogRequestBatch for verification.
type mockStore struct {
	mu      sync.Mutex
	batches [][]*storage.RequestLog
}

func (m *mockStore) LogRequest(ctx context.Context, entry *storage.RequestLog) error {
	return m.LogRequestBatch(ctx, []*storage.RequestLog{entry})
}

func (m *mockStore) LogRequestBatch(ctx context.Context, entries []*storage.RequestLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Make a copy of the slice to avoid data races
	cp := make([]*storage.RequestLog, len(entries))
	copy(cp, entries)
	m.batches = append(m.batches, cp)
	return nil
}

func (m *mockStore) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockStore) NullifyBodyRefs(ctx context.Context, files []string) (int64, error) {
	return 0, nil
}
func (m *mockStore) QueryByTimeRange(ctx context.Context, from, to time.Time) ([]*storage.RequestLog, error) {
	return nil, nil
}
func (m *mockStore) QueryLogs(ctx context.Context, filter storage.LogFilter) ([]*storage.RequestLog, int64, error) {
	return nil, 0, nil
}
func (m *mockStore) QueryByID(ctx context.Context, id string) (*storage.RequestLog, error) {
	return nil, nil
}
func (m *mockStore) Migrate(ctx context.Context) error { return nil }
func (m *mockStore) Close() error                      { return nil }
func (m *mockStore) DistinctMerchants(ctx context.Context, prefix string, limit int) ([]string, error) {
	return nil, nil
}
func (m *mockStore) MethodsByEndpoint(ctx context.Context, from, to time.Time) (map[string][]string, error) {
	return nil, nil
}

func (m *mockStore) allEntries() []*storage.RequestLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []*storage.RequestLog
	for _, batch := range m.batches {
		all = append(all, batch...)
	}
	return all
}

// mockBodyStore records writes for verification.
type mockBodyStore struct {
	mu      sync.Mutex
	entries []*bodies.BodyEntry
	counter int
}

func (m *mockBodyStore) Write(ctx context.Context, entry *bodies.BodyEntry) (string, int64, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	m.counter++
	return "test.jsonl", int64(m.counter * 100), 100, nil
}

func (m *mockBodyStore) Read(ctx context.Context, file string, offset int64, length int) (*bodies.BodyEntry, error) {
	return nil, nil
}

func (m *mockBodyStore) Close() error { return nil }

func (m *mockBodyStore) allEntries() []*bodies.BodyEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*bodies.BodyEntry, len(m.entries))
	copy(cp, m.entries)
	return cp
}

func TestAsyncLogger_LogAndFlush(t *testing.T) {
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())

	logger := NewAsyncLogger(ms, bs, engine, 100)

	entry := &LogEntry{
		Meta: &storage.RequestLog{
			ID:          "req-001",
			Timestamp:   time.Now().UTC(),
			MerchantKey: "merchant-a",
			Method:      "GET",
			Path:        "/orders/v0/orders",
			StatusCode:  200,
			CacheStatus: "MISS",
		},
		Body: &bodies.BodyEntry{
			ID:           "req-001",
			ResponseBody: json.RawMessage(`{"payload":"test"}`),
		},
	}

	logger.Log(entry)
	logger.Close()

	// Verify metadata was written
	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.Equal(t, "req-001", allMeta[0].ID)
	assert.NotEmpty(t, allMeta[0].BodyFile)

	// Verify body was written
	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.Equal(t, "req-001", allBodies[0].ID)
}

func TestAsyncLogger_BatchFlush(t *testing.T) {
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())

	logger := NewAsyncLogger(ms, bs, engine, 1000)

	// Send 250 entries  -  should produce at least 2 batch flushes (batch size 100)
	for i := 0; i < 250; i++ {
		logger.Log(&LogEntry{
			Meta: &storage.RequestLog{
				ID:          GenerateRequestID(),
				Timestamp:   time.Now().UTC(),
				MerchantKey: "merchant-a",
				Method:      "GET",
				Path:        "/test",
				StatusCode:  200,
			},
			Body: &bodies.BodyEntry{
				ID:           GenerateRequestID(),
				ResponseBody: json.RawMessage(`{}`),
			},
		})
	}

	logger.Close()

	allMeta := ms.allEntries()
	assert.Len(t, allMeta, 250)
}

func TestAsyncLogger_CacheHitNoBody(t *testing.T) {
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())

	logger := NewAsyncLogger(ms, bs, engine, 100)

	// Cache hit entry  -  no body
	logger.Log(&LogEntry{
		Meta: &storage.RequestLog{
			ID:           "hit-001",
			Timestamp:    time.Now().UTC(),
			MerchantKey:  "merchant-a",
			Method:       "GET",
			Path:         "/test",
			StatusCode:   200,
			CacheStatus:  "HIT",
			CachedFromID: "original-001",
		},
		Body: nil,
	})

	logger.Close()

	// Metadata should be stored
	allMeta := ms.allEntries()
	require.Len(t, allMeta, 1)
	assert.Equal(t, "HIT", allMeta[0].CacheStatus)
	assert.Equal(t, "original-001", allMeta[0].CachedFromID)
	assert.Empty(t, allMeta[0].BodyFile)

	// No body writes
	assert.Empty(t, bs.allEntries())
}

func TestAsyncLogger_PIIRedaction(t *testing.T) {
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())

	logger := NewAsyncLogger(ms, bs, engine, 100)

	// PII endpoint  -  body should be redacted before storage
	logger.Log(&LogEntry{
		Meta: &storage.RequestLog{
			ID:          "pii-001",
			Timestamp:   time.Now().UTC(),
			MerchantKey: "merchant-a",
			Method:      "GET",
			Path:        "/orders/v0/orders",
			StatusCode:  200,
			PIIRedacted: true,
		},
		Body: &bodies.BodyEntry{
			ID: "pii-001",
			ResponseBody: json.RawMessage(`{"payload":{"Orders":[{"BuyerInfo":{"BuyerEmail":"secret@test.com"},"OrderId":"111"}]}}`),
		},
	})

	logger.Close()

	// Body should have been written with redacted content
	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.NotContains(t, string(allBodies[0].ResponseBody), "secret@test.com")
}

func TestAsyncLogger_DropsWhenFull(t *testing.T) {
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())

	// Very small buffer
	logger := NewAsyncLogger(ms, bs, engine, 1)

	// Pause the worker by filling the channel, then overflowing
	// The worker processes entries, so we need to be fast
	for i := 0; i < 100; i++ {
		logger.Log(&LogEntry{
			Meta: &storage.RequestLog{
				ID:        GenerateRequestID(),
				Timestamp: time.Now().UTC(),
				Method:    "GET",
				Path:      "/test",
			},
		})
	}

	logger.Close()

	// Some entries should have been dropped
	assert.Greater(t, logger.Dropped(), int64(0))
}

func TestAsyncLogger_FullBodyPIIEndpoint(t *testing.T) {
	ms := &mockStore{}
	bs := &mockBodyStore{}
	engine := pii.NewEngine(pii.NewRegistry())

	logger := NewAsyncLogger(ms, bs, engine, 100)

	// Full-body PII endpoint (e.g., /orders/v0/orders/{orderId}/buyerInfo)
	logger.Log(&LogEntry{
		Meta: &storage.RequestLog{
			ID:          "fullpii-001",
			Timestamp:   time.Now().UTC(),
			MerchantKey: "merchant-a",
			Method:      "GET",
			Path:        "/orders/v0/orders/123/buyerInfo",
			StatusCode:  200,
			PIIRedacted: true,
		},
		Body: &bodies.BodyEntry{
			ID:           "fullpii-001",
			ResponseBody: json.RawMessage(`{"BuyerName":"John Doe","BuyerEmail":"john@example.com"}`),
		},
	})

	logger.Close()

	// Full-body PII should be replaced with placeholder
	allBodies := bs.allEntries()
	require.Len(t, allBodies, 1)
	assert.Contains(t, string(allBodies[0].ResponseBody), "redacted")
	assert.NotContains(t, string(allBodies[0].ResponseBody), "John Doe")
}
