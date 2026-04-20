package cache_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runContractTests runs tests that any Cache implementation must pass.
func runContractTests(t *testing.T, c cache.Cache) {
	ctx := context.Background()

	t.Run("GetMiss", func(t *testing.T) {
		got, err := c.Get(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("SetAndGet", func(t *testing.T) {
		resp := &cache.CachedResponse{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(`{"ok":true}`),
			CachedAt:   time.Now(),
			TTL:        5 * time.Minute,
		}
		err := c.Set(ctx, "key1", resp, 5*time.Minute)
		require.NoError(t, err)

		got, err := c.Get(ctx, "key1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 200, got.StatusCode)
		assert.Equal(t, []byte(`{"ok":true}`), got.Body)
		assert.Equal(t, "application/json", got.Headers.Get("Content-Type"))
	})

	t.Run("Delete", func(t *testing.T) {
		resp := &cache.CachedResponse{
			StatusCode: 200,
			Body:       []byte("delete-me"),
			CachedAt:   time.Now(),
			TTL:        5 * time.Minute,
		}
		c.Set(ctx, "del-key", resp, 5*time.Minute)
		err := c.Delete(ctx, "del-key")
		require.NoError(t, err)

		got, err := c.Get(ctx, "del-key")
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("DeleteByPrefix", func(t *testing.T) {
		resp := &cache.CachedResponse{
			StatusCode: 200,
			Body:       []byte("x"),
			CachedAt:   time.Now(),
			TTL:        5 * time.Minute,
		}
		c.Set(ctx, "merchant1:/orders/v0/orders", resp, 5*time.Minute)
		c.Set(ctx, "merchant1:/orders/v0/orders/123", resp, 5*time.Minute)
		c.Set(ctx, "merchant2:/orders/v0/orders", resp, 5*time.Minute)

		err := c.DeleteByPrefix(ctx, "merchant1:/orders")
		require.NoError(t, err)

		got1, _ := c.Get(ctx, "merchant1:/orders/v0/orders")
		got2, _ := c.Get(ctx, "merchant1:/orders/v0/orders/123")
		got3, _ := c.Get(ctx, "merchant2:/orders/v0/orders")
		assert.Nil(t, got1, "merchant1 entry should be deleted")
		assert.Nil(t, got2, "merchant1 entry should be deleted")
		assert.NotNil(t, got3, "merchant2 entry should survive")
	})

	t.Run("Stats", func(t *testing.T) {
		stats := c.Stats()
		assert.GreaterOrEqual(t, stats.Hits, int64(0))
		assert.GreaterOrEqual(t, stats.Misses, int64(0))
	})
}
