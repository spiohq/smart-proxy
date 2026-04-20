package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(maxBytes int64) *cache.MemoryCache {
	return cache.NewMemoryCache(maxBytes)
}

func TestMemoryCache_Contract(t *testing.T) {
	c := newTestCache(1024 * 1024) // 1 MB
	defer c.Close()
	runContractTests(t, c)
}

func TestMemoryCache_ExpirationOnGet(t *testing.T) {
	c := newTestCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	resp := &cache.CachedResponse{
		StatusCode: 200,
		Body:       []byte("expires"),
		CachedAt:   time.Now(),
		TTL:        50 * time.Millisecond,
	}
	c.Set(ctx, "exp-key", resp, 50*time.Millisecond)

	got, err := c.Get(ctx, "exp-key")
	require.NoError(t, err)
	assert.NotNil(t, got)

	time.Sleep(60 * time.Millisecond)

	got, err = c.Get(ctx, "exp-key")
	require.NoError(t, err)
	assert.Nil(t, got, "expired entry should return nil")
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	c := newTestCache(200)
	defer c.Close()

	ctx := context.Background()
	r1 := &cache.CachedResponse{StatusCode: 200, Body: make([]byte, 80), CachedAt: time.Now(), TTL: time.Hour}
	r2 := &cache.CachedResponse{StatusCode: 200, Body: make([]byte, 80), CachedAt: time.Now(), TTL: time.Hour}
	r3 := &cache.CachedResponse{StatusCode: 200, Body: make([]byte, 80), CachedAt: time.Now(), TTL: time.Hour}

	c.Set(ctx, "k1", r1, time.Hour)
	c.Set(ctx, "k2", r2, time.Hour)
	c.Set(ctx, "k3", r3, time.Hour)

	got1, _ := c.Get(ctx, "k1")
	got3, _ := c.Get(ctx, "k3")
	assert.Nil(t, got1, "k1 should be evicted as LRU")
	assert.NotNil(t, got3, "k3 should exist")
}

func TestMemoryCache_StatsTracking(t *testing.T) {
	c := newTestCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	c.Get(ctx, "miss1")
	c.Get(ctx, "miss2")

	resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("x"), CachedAt: time.Now(), TTL: time.Hour}
	c.Set(ctx, "hit1", resp, time.Hour)
	c.Get(ctx, "hit1")

	stats := c.Stats()
	assert.Equal(t, int64(1), stats.Hits)
	assert.Equal(t, int64(2), stats.Misses)
	assert.Equal(t, int64(1), stats.EntryCount)
	assert.Greater(t, stats.BytesUsed, int64(0))
}

func TestMemoryCache_DeleteByPrefixPartialMatch(t *testing.T) {
	c := newTestCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("x"), CachedAt: time.Now(), TTL: time.Hour}

	// Set entries with similar but distinct prefixes
	c.Set(ctx, "m1:GET:/orders/v0/orders", resp, time.Hour)
	c.Set(ctx, "m1:GET:/orders/v0/orders/123", resp, time.Hour)
	c.Set(ctx, "m1:GET:/orders/v0/ordersBULK", resp, time.Hour) // should also match prefix "/orders/v0/orders"

	c.DeleteByPrefix(ctx, "m1:GET:/orders/v0/orders")

	got1, _ := c.Get(ctx, "m1:GET:/orders/v0/orders")
	got2, _ := c.Get(ctx, "m1:GET:/orders/v0/orders/123")
	got3, _ := c.Get(ctx, "m1:GET:/orders/v0/ordersBULK")
	assert.Nil(t, got1)
	assert.Nil(t, got2)
	assert.Nil(t, got3, "prefix match includes keys that start with the prefix string")
}

func TestMemoryCache_StatsEvictions(t *testing.T) {
	// Very small cache to force evictions
	c := newTestCache(300)
	defer c.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		resp := &cache.CachedResponse{StatusCode: 200, Body: make([]byte, 100), CachedAt: time.Now(), TTL: time.Hour}
		c.Set(ctx, "evict-"+string(rune('A'+i)), resp, time.Hour)
	}

	stats := c.Stats()
	assert.Greater(t, stats.Evictions, int64(0), "should have evicted entries")
}

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	c := newTestCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("data"), CachedAt: time.Now(), TTL: time.Hour}
			key := "concurrent-" + string(rune('A'+id))
			c.Set(ctx, key, resp, time.Hour)
			c.Get(ctx, key)
			c.Delete(ctx, key)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
