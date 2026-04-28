package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func testRequestLog(id, merchantKey string) *RequestLog {
	return &RequestLog{
		ID:                id,
		Timestamp:         time.Now().UTC(),
		MerchantKey:       merchantKey,
		Region:            "eu",
		Method:            "GET",
		Path:              "/orders/v0/orders",
		StatusCode:        200,
		CacheStatus:       "MISS",
		UpstreamLatencyMs: 150,
		TotalLatencyMs:    155,
	}
}

func TestSQLiteStore_LogRequest(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entry := testRequestLog("req-001", "merchant-a")
	entry.BodyFile = "2026-03-25-14.jsonl"
	entry.BodyOffset = 1024
	entry.BodyLength = 512

	err := store.LogRequest(ctx, entry)
	require.NoError(t, err)

	// Verify it was inserted
	var id, merchantKey, bodyFile string
	var statusCode int
	var bodyOffset int64
	var bodyLength int
	err = store.db.QueryRowContext(ctx,
		"SELECT id, merchant_key, status_code, body_file, body_offset, body_length FROM request_logs WHERE id = ?",
		"req-001",
	).Scan(&id, &merchantKey, &statusCode, &bodyFile, &bodyOffset, &bodyLength)
	require.NoError(t, err)
	assert.Equal(t, "req-001", id)
	assert.Equal(t, "merchant-a", merchantKey)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, "2026-03-25-14.jsonl", bodyFile)
	assert.Equal(t, int64(1024), bodyOffset)
	assert.Equal(t, 512, bodyLength)
}

func TestSQLiteStore_LogRequestBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entries := []*RequestLog{
		testRequestLog("batch-001", "merchant-a"),
		testRequestLog("batch-002", "merchant-a"),
		testRequestLog("batch-003", "merchant-b"),
	}

	err := store.LogRequestBatch(ctx, entries)
	require.NoError(t, err)

	var count int
	err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestSQLiteStore_LogRequestBatch_Empty(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.LogRequestBatch(ctx, nil)
	require.NoError(t, err)
}

func TestSQLiteStore_PurgeOlderThan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Insert old and new entries
	old := testRequestLog("old-001", "merchant-a")
	old.Timestamp = time.Now().UTC().Add(-48 * time.Hour)

	recent := testRequestLog("new-001", "merchant-a")
	recent.Timestamp = time.Now().UTC()

	require.NoError(t, store.LogRequest(ctx, old))
	require.NoError(t, store.LogRequest(ctx, recent))

	// Purge entries older than 24h
	deleted, err := store.PurgeOlderThan(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Verify only recent entry remains
	var count int
	err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestSQLiteStore_PurgeOlderThan_NothingToPurge(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	deleted, err := store.PurgeOlderThan(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestSQLiteStore_QueryByTimeRange(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	old := testRequestLog("old-001", "merchant-a")
	old.Timestamp = now.Add(-2 * time.Hour)
	require.NoError(t, store.LogRequest(ctx, old))

	recent := testRequestLog("recent-001", "merchant-a")
	recent.Timestamp = now.Add(-30 * time.Minute)
	require.NoError(t, store.LogRequest(ctx, recent))

	future := testRequestLog("future-001", "merchant-a")
	future.Timestamp = now.Add(1 * time.Hour)
	require.NoError(t, store.LogRequest(ctx, future))

	// Query last hour
	results, err := store.QueryByTimeRange(ctx, now.Add(-1*time.Hour), now)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "recent-001", results[0].ID)
}

func TestSQLiteStore_LogRequest_WithCachedFromID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entry := testRequestLog("hit-001", "merchant-a")
	entry.CacheStatus = "HIT"
	entry.CachedFromID = "original-001"

	err := store.LogRequest(ctx, entry)
	require.NoError(t, err)

	var cachedFromID sql.NullString
	err = store.db.QueryRowContext(ctx,
		"SELECT cached_from_id FROM request_logs WHERE id = ?", "hit-001",
	).Scan(&cachedFromID)
	require.NoError(t, err)
	assert.True(t, cachedFromID.Valid)
	assert.Equal(t, "original-001", cachedFromID.String)
}

func TestSQLiteStore_QueryByID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entry := testRequestLog("find-me", "merchant-a")
	require.NoError(t, store.LogRequest(ctx, entry))

	found, err := store.QueryByID(ctx, "find-me")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "find-me", found.ID)
	assert.Equal(t, "merchant-a", found.MerchantKey)

	// Not found
	notFound, err := store.QueryByID(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestSQLiteStore_QueryLogs_Basic(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		e := testRequestLog(fmt.Sprintf("log-%03d", i), "merchant-a")
		e.Timestamp = now.Add(-time.Duration(i) * time.Minute)
		require.NoError(t, store.LogRequest(ctx, e))
	}

	rows, total, err := store.QueryLogs(ctx, LogFilter{
		From:  now.Add(-10 * time.Minute),
		To:    now.Add(time.Minute),
		Limit: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, rows, 3)
}

func TestSQLiteStore_QueryLogs_Filters(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()

	e1 := testRequestLog("m1", "merchant-a")
	e1.Timestamp = now
	e1.Region = "eu"
	e1.StatusCode = 200
	e1.TotalLatencyMs = 50
	require.NoError(t, store.LogRequest(ctx, e1))

	e2 := testRequestLog("m2", "merchant-b")
	e2.Timestamp = now
	e2.Region = "na"
	e2.StatusCode = 500
	e2.TotalLatencyMs = 500
	e2.Path = "/catalog/v0/items"
	e2.CacheStatus = "HIT"
	require.NoError(t, store.LogRequest(ctx, e2))

	// Filter by merchant
	rows, total, err := store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Merchant: "merchant-a", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, rows, 1)
	assert.Equal(t, "m1", rows[0].ID)

	// Filter by region
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Region: "na", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m2", rows[0].ID)

	// Filter by status bucket "5xx"
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Status: "5xx", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m2", rows[0].ID)

	// Filter by exact status code
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Status: "200", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m1", rows[0].ID)

	// Filter by endpoint prefix
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Endpoint: "/catalog", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m2", rows[0].ID)

	// Filter by latency range
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		MinLatency: 100, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m2", rows[0].ID)

	// Filter by merchant prefix
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Merchant: "merchant-a", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m1", rows[0].ID)

	// Filter by merchant prefix (partial)
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		Merchant: "merchant", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)

	// Filter by cache status
	rows, total, err = store.QueryLogs(ctx, LogFilter{
		From: now.Add(-time.Minute), To: now.Add(time.Minute),
		CacheStatus: "MISS", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "m1", rows[0].ID)
}

func TestSQLiteStore_DistinctMerchants(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	// Insert entries with different merchants
	for i, m := range []struct{ id, merchant string }{
		{"dm-1", "SELLER_ABC123"},
		{"dm-2", "SELLER_ABC123"}, // duplicate merchant
		{"dm-3", "SELLER_XYZ789"},
		{"dm-4", "AGENCY_DEMO"},
		{"dm-5", ""},              // empty merchant (should be excluded)
	} {
		e := testRequestLog(m.id, m.merchant)
		e.Timestamp = now.Add(time.Duration(i) * time.Second)
		require.NoError(t, store.LogRequest(ctx, e))
	}

	// No prefix  -  returns all non-empty merchants, most recent first
	merchants, err := store.DistinctMerchants(ctx, "", 20)
	require.NoError(t, err)
	assert.Len(t, merchants, 3)
	// Most recent first: AGENCY_DEMO (dm-4), SELLER_XYZ789 (dm-3), SELLER_ABC123 (dm-2)
	assert.Equal(t, "AGENCY_DEMO", merchants[0])
	assert.Equal(t, "SELLER_XYZ789", merchants[1])
	assert.Equal(t, "SELLER_ABC123", merchants[2])

	// Prefix filter
	merchants, err = store.DistinctMerchants(ctx, "SELLER", 20)
	require.NoError(t, err)
	assert.Len(t, merchants, 2)
	assert.Equal(t, "SELLER_XYZ789", merchants[0])
	assert.Equal(t, "SELLER_ABC123", merchants[1])

	// Prefix filter  -  exact match
	merchants, err = store.DistinctMerchants(ctx, "AGENCY_DEMO", 20)
	require.NoError(t, err)
	assert.Len(t, merchants, 1)
	assert.Equal(t, "AGENCY_DEMO", merchants[0])

	// Prefix filter  -  no match
	merchants, err = store.DistinctMerchants(ctx, "NONEXISTENT", 20)
	require.NoError(t, err)
	assert.Empty(t, merchants)

	// Limit
	merchants, err = store.DistinctMerchants(ctx, "", 1)
	require.NoError(t, err)
	assert.Len(t, merchants, 1)
}

func TestSQLiteStore_NullifyBodyRefs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	a := testRequestLog("req-a", "m")
	a.BodyFile = "2026-04-20-10.jsonl"
	a.BodyOffset = 100
	a.BodyLength = 200
	b := testRequestLog("req-b", "m")
	b.BodyFile = "2026-04-20-11.jsonl"
	b.BodyOffset = 300
	b.BodyLength = 400
	c := testRequestLog("req-c", "m")
	c.BodyFile = "2026-04-20-12.jsonl"
	c.BodyOffset = 500
	c.BodyLength = 600
	require.NoError(t, store.LogRequestBatch(ctx, []*RequestLog{a, b, c}))

	n, err := store.NullifyBodyRefs(ctx, []string{"2026-04-20-10.jsonl", "2026-04-20-12.jsonl"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	got, err := store.QueryByID(ctx, "req-a")
	require.NoError(t, err)
	assert.Equal(t, "", got.BodyFile)
	assert.Equal(t, int64(0), got.BodyOffset)
	assert.Equal(t, 0, got.BodyLength)

	got, err = store.QueryByID(ctx, "req-b")
	require.NoError(t, err)
	assert.Equal(t, "2026-04-20-11.jsonl", got.BodyFile)
	assert.Equal(t, int64(300), got.BodyOffset)
	assert.Equal(t, 400, got.BodyLength)

	got, err = store.QueryByID(ctx, "req-c")
	require.NoError(t, err)
	assert.Equal(t, "", got.BodyFile)
}

func TestSQLiteStore_NullifyBodyRefs_EmptyNoOp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	n, err := store.NullifyBodyRefs(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func TestNewSQLiteStore_FileModeIs0o600(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// All three SQLite-managed files must be 0o600. The main DB file is
	// chmodded by NewSQLiteStore directly; the WAL and SHM files are
	// created by SQLite with the process umask and have to be chmodded
	// separately by NewSQLiteStore after the first PRAGMA journal_mode=WAL.
	for _, name := range []string{"test.db", "test.db-wal", "test.db-shm"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "%s must exist after NewSQLiteStore", name)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
			"%s must be 0o600 to keep WAL/SHM PII out of other users' reach", name)
	}
}

func TestSQLiteStore_Maintain(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Seed, purge, then maintain; the combination exercises incremental_vacuum
	// against freed pages. We assert the call succeeds and that the store is
	// still usable afterwards (a broken PRAGMA would leave the DB in a bad state).
	for i := 0; i < 50; i++ {
		e := testRequestLog(fmt.Sprintf("m-%03d", i), "merchant-a")
		e.Timestamp = time.Now().UTC().Add(-48 * time.Hour)
		require.NoError(t, store.LogRequest(ctx, e))
	}
	_, err := store.PurgeOlderThan(ctx, 24*time.Hour)
	require.NoError(t, err)

	require.NoError(t, store.Maintain(ctx))

	// Store still accepts writes and reads after maintenance.
	fresh := testRequestLog("fresh", "merchant-a")
	require.NoError(t, store.LogRequest(ctx, fresh))
	got, err := store.QueryByID(ctx, "fresh")
	require.NoError(t, err)
	require.NotNil(t, got)
}
