package prommetrics

import (
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
)

// CacheStatsProvider exposes cache stats for metric collection.
type CacheStatsProvider interface {
	Stats() cache.CacheStats
}

// StartCollectors starts a background goroutine that periodically updates
// gauge metrics from the cache and rate limiter. Call the returned stop
// function to terminate the goroutine.
func StartCollectors(m *Metrics, cacheProvider CacheStatsProvider, limiter *ratelimit.Limiter, interval time.Duration) (stop func()) {
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if cacheProvider != nil {
					stats := cacheProvider.Stats()
					m.CacheSizeBytes.Set(float64(stats.BytesUsed))
					m.CacheEntries.Set(float64(stats.EntryCount))
					m.CacheEvictionsTotal.Add(0) // keep metric alive; actual increments come from middleware
				}
				if limiter != nil {
					m.RateLimitBucketsActive.Set(float64(limiter.BucketCount()))
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}
