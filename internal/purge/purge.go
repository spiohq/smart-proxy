package purge

import (
	"context"
	"log/slog"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/storage"
)

// MetadataPurgeJob returns a scheduler-compatible function that purges old request logs.
func MetadataPurgeJob(store storage.Store, auditLogger *audit.AuditLogger, retention time.Duration) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		count, err := store.PurgeOlderThan(ctx, retention)
		if err != nil {
			slog.Error("metadata purge failed", "error", err)
			return err
		}
		if count > 0 {
			slog.Info("metadata purged", "count", count, "retention", retention)
			auditLogger.Log(ctx, "purge_metadata", "purge", "request logs purged",
				map[string]any{"count": count, "retention": retention.String()})
		}
		return nil
	}
}

// AuditPurgeJob returns a scheduler-compatible function that purges old audit entries.
// Does not audit-log itself (avoids infinite recursion).
func AuditPurgeJob(auditStore audit.Store, retention time.Duration) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		count, err := auditStore.PurgeOlderThan(ctx, retention)
		if err != nil {
			slog.Error("audit purge failed", "error", err)
			return err
		}
		if count > 0 {
			slog.Info("audit entries purged", "count", count, "retention", retention)
		}
		return nil
	}
}
