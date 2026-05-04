package logging

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/endpoint"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/spiohq/smart-proxy/internal/tokenstore"
)

const (
	defaultBatchSize  = 100
	defaultFlushTimer = 1 * time.Second
)

// AsyncLogger receives log entries on a buffered channel and writes them
// to storage in batches. Non-blocking  -  never slows down the proxy.
type AsyncLogger struct {
	store      storage.Store
	bodyStore  bodies.BodyStore
	piiEngine  *pii.Engine
	tokenStore *tokenstore.Store
	entries    chan *LogEntry
	wg         sync.WaitGroup
	dropped    atomic.Int64
	closeOnce  sync.Once
}

// NewAsyncLogger creates a logger and starts the background worker.
func NewAsyncLogger(store storage.Store, bodyStore bodies.BodyStore, piiEngine *pii.Engine, bufferSize int) *AsyncLogger {
	l := &AsyncLogger{
		store:     store,
		bodyStore: bodyStore,
		piiEngine: piiEngine,
		entries:   make(chan *LogEntry, bufferSize),
	}
	l.wg.Add(1)
	go l.worker()
	return l
}

// NewAsyncLoggerWithTokenStore creates a logger with an attached token store
// for capturing access tokens before header redaction.
func NewAsyncLoggerWithTokenStore(store storage.Store, bodyStore bodies.BodyStore, piiEngine *pii.Engine, queueSize int, ts *tokenstore.Store) *AsyncLogger {
	l := NewAsyncLogger(store, bodyStore, piiEngine, queueSize)
	l.tokenStore = ts
	return l
}

// TokenStore returns the attached token store, or nil if none was configured.
func (l *AsyncLogger) TokenStore() *tokenstore.Store {
	return l.tokenStore
}

// Log sends an entry to the async pipeline. Non-blocking  -  drops if channel is full or closed.
func (l *AsyncLogger) Log(entry *LogEntry) {
	// Recover from send-on-closed-channel if Close() races with Log().
	defer func() {
		if r := recover(); r != nil {
			l.dropped.Add(1)
		}
	}()
	select {
	case l.entries <- entry:
	default:
		l.dropped.Add(1)
	}
}

// Close closes the channel and waits for the worker to drain all remaining entries.
// Safe to call multiple times.
func (l *AsyncLogger) Close() {
	l.closeOnce.Do(func() {
		close(l.entries)
	})
	l.wg.Wait()
}

// Dropped returns the number of entries dropped due to full channel.
func (l *AsyncLogger) Dropped() int64 {
	return l.dropped.Load()
}

func (l *AsyncLogger) worker() {
	defer l.wg.Done()
	batch := make([]*LogEntry, 0, defaultBatchSize)
	ticker := time.NewTicker(defaultFlushTimer)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-l.entries:
			if !ok {
				l.flush(batch)
				return
			}
			batch = append(batch, entry)
			if len(batch) >= defaultBatchSize {
				l.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				l.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (l *AsyncLogger) flush(batch []*LogEntry) {
	if len(batch) == 0 {
		return
	}

	ctx := context.Background()

	for _, entry := range batch {
		if entry.Body != nil {
			l.redactBody(entry)

			file, offset, length, err := l.bodyStore.Write(ctx, entry.Body)
			if err != nil {
				slog.Error("body write failed", "id", entry.Meta.ID, "error", err)
			} else {
				entry.Meta.BodyFile = file
				entry.Meta.BodyOffset = offset
				entry.Meta.BodyLength = length
			}
		}
	}

	metas := make([]*storage.RequestLog, len(batch))
	for i, e := range batch {
		metas[i] = e.Meta
	}
	if err := l.store.LogRequestBatch(ctx, metas); err != nil {
		slog.Error("metadata batch write failed", "count", len(metas), "error", err)
	}
}

func (l *AsyncLogger) redactBody(entry *LogEntry) {
	if (!entry.Meta.PIIRedactedRequest && !entry.Meta.PIIRedactedResponse) || l.piiEngine == nil {
		return
	}

	classifiedPath, known := endpoint.ClassifyKnown(entry.Meta.Path)

	// Response-side redaction (existing semantics, gated on the response flag).
	if entry.Meta.PIIRedactedResponse {
		if l.piiEngine.Registry().IsFullBodyPII(classifiedPath, known) {
			entry.Body.ResponseBody = json.RawMessage(l.piiEngine.RedactFullBody(classifiedPath))
		} else if entry.Body.ResponseBody != nil {
			redacted, _ := l.piiEngine.RedactForLogging(classifiedPath, []byte(entry.Body.ResponseBody))
			entry.Body.ResponseBody = json.RawMessage(redacted)
		}
	}

	// Request-side redaction (F-02; gated on the request flag).
	// Use entry.RequestBodyEndpoint (set by middleware) rather than classifiedPath:
	// for POST messaging endpoints the method-blind classifier returns the GET
	// variant, but the request-body rules are keyed on action-specific patterns.
	if entry.Meta.PIIRedactedRequest && entry.Body.RequestBody != nil {
		reqEndpoint := entry.RequestBodyEndpoint
		if reqEndpoint == "" {
			// Defensive fallback: middleware should always set this when it
			// sets PIIRedactedRequest. If a future caller bypasses the
			// middleware (e.g. legacy-record reprocessing), fall back to
			// the raw classified path so the engine can at least try.
			reqEndpoint = classifiedPath
		}
		redacted, ok := l.piiEngine.RedactRequestBodyForLogging(reqEndpoint, []byte(entry.Body.RequestBody))
		if ok {
			entry.Body.RequestBody = json.RawMessage(redacted)
		} else if l.piiEngine.Registry().IsFullBodyPII(classifiedPath, known) {
			// Fail-closed unknown path: only reached when the path was not
			// recognised by the classifier. Mirror the response-side behavior
			// and replace the body with a placeholder.
			entry.Body.RequestBody = json.RawMessage(l.piiEngine.RedactFullBody(reqEndpoint))
		}
		// else: rule lookup found rules but none matched the body fields
		// (e.g. caller sent a partial schema); leave the body as-is. This
		// is the same behavior RedactForLogging exhibits on the response
		// side when no rule matched the actual JSON.
	}
}
