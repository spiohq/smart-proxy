package audit_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// TestDPPWarningRoundTrip verifies that the EventDPPComplianceWarning constant
// flows through the audit store unchanged. This is the contract the audit-log
// query path will rely on at audit time.
func TestDPPWarningRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE audit_log (
		id TEXT PRIMARY KEY,
		timestamp TIMESTAMP NOT NULL,
		event_type TEXT NOT NULL,
		source TEXT NOT NULL,
		message TEXT NOT NULL,
		metadata TEXT
	)`)
	require.NoError(t, err)

	store := audit.NewSQLiteStore(db)
	logger := audit.NewAuditLogger(store)

	err = logger.Log(context.Background(), audit.EventDPPComplianceWarning, "config",
		"SP_PROXY_PII_FAIL_CLOSED=false in production", nil)
	require.NoError(t, err)

	events, _, err := store.QueryAuditEvents(context.Background(), audit.AuditFilter{
		EventType: audit.EventDPPComplianceWarning,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, audit.EventDPPComplianceWarning, events[0].EventType)
	assert.Contains(t, events[0].Message, "SP_PROXY_PII_FAIL_CLOSED")
}
