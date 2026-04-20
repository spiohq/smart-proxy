package cache

import (
	"context"
	"net/http"
	"time"
)

// Cache is the interface for response caching backends.
// Phase 3 provides MemoryCache; Redis/Badger can implement this later.
type Cache interface {
	Get(ctx context.Context, key string) (*CachedResponse, error)
	Set(ctx context.Context, key string, resp *CachedResponse, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	DeleteByPrefix(ctx context.Context, prefix string) error
	Stats() CacheStats
	Close() error
}

// CachedResponse holds a cached HTTP response.
type CachedResponse struct {
	StatusCode      int
	Headers         http.Header
	Body            []byte
	CachedAt        time.Time
	TTL             time.Duration
	SourceRequestID string // ID of the request that generated this cached response
}

// CacheStats holds cache hit/miss/eviction counters.
type CacheStats struct {
	Hits       int64
	Misses     int64
	Evictions  int64
	BytesUsed  int64
	EntryCount int64
}
