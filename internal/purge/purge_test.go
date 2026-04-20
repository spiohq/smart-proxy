package purge

import (
	"context"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func newTestStores(t *testing.T) (*storage.SQLiteStore, *audit.SQLiteStore) {
	t.Helper()
	logStore, err := storage.NewSQLiteStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { logStore.Close() })

	db := logStore.DB()

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY, timestamp TIMESTAMP NOT NULL,
			event_type TEXT NOT NULL, source TEXT NOT NULL,
			message TEXT NOT NULL, metadata TEXT
		)`)
	require.NoError(t, err)

	auditStore := audit.NewSQLiteStore(db)
	return logStore, auditStore
}

func TestMetadataPurgeJob(t *testing.T) {
	logStore, auditStore := newTestStores(t)
	ctx := context.Background()
	auditLogger := audit.NewAuditLogger(auditStore)

	// Insert old and recent logs
	old := &storage.RequestLog{
		ID: "old-001", Timestamp: time.Now().UTC().Add(-48 * time.Hour),
		MerchantKey: "m", Method: "GET", Path: "/test", StatusCode: 200,
	}
	recent := &storage.RequestLog{
		ID: "recent-001", Timestamp: time.Now().UTC(),
		MerchantKey: "m", Method: "GET", Path: "/test", StatusCode: 200,
	}
	require.NoError(t, logStore.LogRequest(ctx, old))
	require.NoError(t, logStore.LogRequest(ctx, recent))

	job := MetadataPurgeJob(logStore, auditLogger, 24*time.Hour)
	err := job(ctx)
	require.NoError(t, err)

	// Verify old was purged
	results, err := logStore.QueryByTimeRange(ctx, time.Time{}, time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "recent-001", results[0].ID)
}

func TestAuditPurgeJob(t *testing.T) {
	_, auditStore := newTestStores(t)
	ctx := context.Background()

	// Insert old and recent audit events
	old := &audit.AuditEvent{
		ID: "old-001", Timestamp: time.Now().UTC().Add(-400 * 24 * time.Hour),
		EventType: "startup", Source: "main", Message: "old",
	}
	recent := &audit.AuditEvent{
		ID: "recent-001", Timestamp: time.Now().UTC(),
		EventType: "startup", Source: "main", Message: "recent",
	}
	require.NoError(t, auditStore.LogAuditEvent(ctx, old))
	require.NoError(t, auditStore.LogAuditEvent(ctx, recent))

	job := AuditPurgeJob(auditStore, 365*24*time.Hour)
	err := job(ctx)
	require.NoError(t, err)

	// Verify purge ran without error. The old event should be gone.
	count, err := auditStore.PurgeOlderThan(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the recent event should remain")
}
