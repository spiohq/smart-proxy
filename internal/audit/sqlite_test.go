package audit

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`CREATE TABLE audit_log (
		id TEXT PRIMARY KEY,
		timestamp TIMESTAMP NOT NULL,
		event_type TEXT NOT NULL,
		source TEXT NOT NULL,
		message TEXT NOT NULL,
		metadata TEXT
	)`)
	require.NoError(t, err)
	return db
}

func TestSQLiteStore_LogAuditEvent(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	event := &AuditEvent{
		ID:        "test-001",
		Timestamp: time.Now().UTC(),
		EventType: "startup",
		Source:    "main",
		Message:  "proxy starting",
		Metadata:  map[string]any{"version": "dev"},
	}

	err := store.LogAuditEvent(ctx, event)
	require.NoError(t, err)

	var id, eventType, source, message string
	var metadata sql.NullString
	err = db.QueryRowContext(ctx,
		"SELECT id, event_type, source, message, metadata FROM audit_log WHERE id = ?", "test-001",
	).Scan(&id, &eventType, &source, &message, &metadata)
	require.NoError(t, err)
	assert.Equal(t, "startup", eventType)
	assert.Equal(t, "main", source)
	assert.Equal(t, "proxy starting", message)
	assert.True(t, metadata.Valid)
	assert.Contains(t, metadata.String, "dev")
}

func TestSQLiteStore_PurgeOlderThan(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	// Insert old event
	old := &AuditEvent{
		ID:        "old-001",
		Timestamp: time.Now().UTC().Add(-48 * time.Hour),
		EventType: "startup",
		Source:    "main",
		Message:  "old event",
	}
	require.NoError(t, store.LogAuditEvent(ctx, old))

	// Insert recent event
	recent := &AuditEvent{
		ID:        "recent-001",
		Timestamp: time.Now().UTC(),
		EventType: "startup",
		Source:    "main",
		Message:  "recent event",
	}
	require.NoError(t, store.LogAuditEvent(ctx, recent))

	// Purge events older than 24h
	count, err := store.PurgeOlderThan(ctx, 24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Verify only recent remains
	var remaining int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log").Scan(&remaining)
	assert.Equal(t, 1, remaining)
}

func TestSQLiteStore_QueryAuditEvents(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		evt := &AuditEvent{
			ID:        fmt.Sprintf("evt-%03d", i),
			Timestamp: now.Add(-time.Duration(i) * time.Minute),
			EventType: "startup",
			Source:    "main",
			Message:   fmt.Sprintf("event %d", i),
		}
		if i%2 == 0 {
			evt.EventType = "purge"
		}
		require.NoError(t, store.LogAuditEvent(ctx, evt))
	}

	// Basic query
	rows, total, err := store.QueryAuditEvents(ctx, AuditFilter{
		From: now.Add(-15 * time.Minute), To: now.Add(time.Minute),
		Limit: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(10), total)
	assert.Len(t, rows, 5)

	// Filter by event type
	rows, total, err = store.QueryAuditEvents(ctx, AuditFilter{
		From: now.Add(-15 * time.Minute), To: now.Add(time.Minute),
		EventType: "purge", Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, rows, 5)
	for _, r := range rows {
		assert.Equal(t, "purge", r.EventType)
	}

	// Pagination offset
	rows, total, err = store.QueryAuditEvents(ctx, AuditFilter{
		From: now.Add(-15 * time.Minute), To: now.Add(time.Minute),
		Limit: 50, Offset: 8,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(10), total)
	assert.Len(t, rows, 2)
}

func TestAuditLogger_NilSafe(t *testing.T) {
	var logger *AuditLogger
	err := logger.Log(context.Background(), "test", "test", "message", nil)
	assert.NoError(t, err)
}

func TestAuditLogger_Log(t *testing.T) {
	db := newTestDB(t)
	store := NewSQLiteStore(db)
	logger := NewAuditLogger(store)
	ctx := context.Background()

	err := logger.Log(ctx, "shutdown", "main", "stopping", map[string]any{"reason": "sigterm"})
	require.NoError(t, err)

	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log").Scan(&count)
	assert.Equal(t, 1, count)
}

func TestMarshalMetadata_NilMap(t *testing.T) {
	assert.Equal(t, "", MarshalMetadata(nil))
}

func TestMarshalMetadata_ValidMap(t *testing.T) {
	result := MarshalMetadata(map[string]any{"count": 42})
	assert.Contains(t, result, "42")
}
