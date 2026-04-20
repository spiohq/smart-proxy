package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/stretchr/testify/assert"
)

func TestExtractResourcePrefix(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/listings/2021-08-01/items/SELLER/SKU123", "/listings/2021-08-01/items/SELLER"},
		{"/orders/v0/orders/123-456-789", "/orders/v0/orders"},
		{"/orders/v0/orders", "/orders/v0/orders"},
		{"/catalog/2022-04-01/items/B08N5WRWNW", "/catalog/2022-04-01/items"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, cache.ExtractResourcePrefix(tt.path))
		})
	}
}

func TestInvalidateOnMutation_DeletesPrefixEntries(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("data"), CachedAt: time.Now(), TTL: time.Hour}

	// Pre-populate cache with entries using real cache key format (merchantKey:METHOD:path)
	c.Set(ctx, "merchant1:GET:/listings/2021-08-01/items/SELLER", resp, time.Hour)
	c.Set(ctx, "merchant1:GET:/listings/2021-08-01/items/SELLER/SKU1", resp, time.Hour)
	c.Set(ctx, "merchant1:GET:/listings/2021-08-01/items/SELLER/SKU2", resp, time.Hour)
	c.Set(ctx, "merchant2:GET:/listings/2021-08-01/items/OTHER", resp, time.Hour)

	// Mutation: PATCH /listings/2021-08-01/items/SELLER/SKU1
	// extractResourcePrefix → /listings/2021-08-01/items/SELLER
	// DeleteByPrefix("merchant1:GET:/listings/2021-08-01/items/SELLER")
	cache.InvalidateOnMutation(c, "merchant1", "PATCH", "/listings/2021-08-01/items/SELLER/SKU1")

	// merchant1's listing entries under /SELLER should be gone
	got1, _ := c.Get(ctx, "merchant1:GET:/listings/2021-08-01/items/SELLER")
	got2, _ := c.Get(ctx, "merchant1:GET:/listings/2021-08-01/items/SELLER/SKU1")
	got3, _ := c.Get(ctx, "merchant1:GET:/listings/2021-08-01/items/SELLER/SKU2")
	assert.Nil(t, got1)
	assert.Nil(t, got2)
	assert.Nil(t, got3)

	// merchant2's entries should survive
	got4, _ := c.Get(ctx, "merchant2:GET:/listings/2021-08-01/items/OTHER")
	assert.NotNil(t, got4)
}

func TestExtractResourcePrefix_TrailingSlash(t *testing.T) {
	assert.Equal(t, "/listings/2021-08-01/items/SELLER",
		cache.ExtractResourcePrefix("/listings/2021-08-01/items/SELLER/SKU1/"))
}

func TestExtractResourcePrefix_ShortPath(t *testing.T) {
	// Paths with 4 or fewer segments should not be trimmed
	tests := []struct {
		path     string
		expected string
	}{
		{"/orders", "/orders"},
		{"/orders/v0", "/orders/v0"},
		{"/orders/v0/orders", "/orders/v0/orders"},
		{"/a/b/c/d", "/a/b/c"}, // 5 parts after split (["","a","b","c","d"]) → trims last
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, cache.ExtractResourcePrefix(tt.path))
		})
	}
}

func TestInvalidateOnMutation_DeleteMethod(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("data"), CachedAt: time.Now(), TTL: time.Hour}

	c.Set(ctx, "m1:GET:/listings/2021-08-01/items/SELLER/SKU1", resp, time.Hour)
	c.Set(ctx, "m1:GET:/listings/2021-08-01/items/SELLER/SKU2", resp, time.Hour)

	cache.InvalidateOnMutation(c, "m1", "DELETE", "/listings/2021-08-01/items/SELLER/SKU1")

	got1, _ := c.Get(ctx, "m1:GET:/listings/2021-08-01/items/SELLER/SKU1")
	got2, _ := c.Get(ctx, "m1:GET:/listings/2021-08-01/items/SELLER/SKU2")
	assert.Nil(t, got1, "DELETE should invalidate the entry")
	assert.Nil(t, got2, "DELETE should invalidate sibling entries with same prefix")
}

func TestInvalidateOnMutation_PUTMethod(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("data"), CachedAt: time.Now(), TTL: time.Hour}

	c.Set(ctx, "m1:GET:/listings/2021-08-01/items/SELLER/SKU1", resp, time.Hour)

	cache.InvalidateOnMutation(c, "m1", "PUT", "/listings/2021-08-01/items/SELLER/SKU1")

	got, _ := c.Get(ctx, "m1:GET:/listings/2021-08-01/items/SELLER/SKU1")
	assert.Nil(t, got, "PUT should invalidate the entry")
}

func TestInvalidateOnMutation_SkipsGET(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	ctx := context.Background()
	resp := &cache.CachedResponse{StatusCode: 200, Body: []byte("data"), CachedAt: time.Now(), TTL: time.Hour}
	c.Set(ctx, "m1:GET:/orders/v0/orders", resp, time.Hour)

	cache.InvalidateOnMutation(c, "m1", "GET", "/orders/v0/orders")

	got, _ := c.Get(ctx, "m1:GET:/orders/v0/orders")
	assert.NotNil(t, got, "GET should not trigger invalidation")
}
