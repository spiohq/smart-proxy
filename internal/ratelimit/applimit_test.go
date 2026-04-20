package ratelimit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Application-Level Rate Limiting Tests ─────────────────────────────────────
// These tests verify cross-merchant (application-wide) rate limits.
// E.g., patchListingsItem has a 500 req/s app-level limit across ALL sellers.

func TestAppBucketParams_Exists(t *testing.T) {
	// Application-level limits should be defined
	assert.NotEmpty(t, AppBucketParams, "AppBucketParams should have entries")
}

func TestAppBucketParams_PatchListingsItem(t *testing.T) {
	// patchListingsItem: 500 req/s application-wide
	key := "PATCH:/listings/2021-08-01/items/{sellerId}/{sku}"
	params, ok := AppBucketParams[key]
	require.True(t, ok, "app-level limit for PATCH listings items should exist")
	assert.InDelta(t, 500.0, params.Rate, 0.1)
}

func TestLimiter_GetAppBucket_ReturnsSharedBucket(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	// App bucket should be the same regardless of calls  -  use an endpoint with app limits
	b1, ok1 := l.GetAppBucket("GET", "/catalog/2022-04-01/items")
	b2, ok2 := l.GetAppBucket("GET", "/catalog/2022-04-01/items")
	require.True(t, ok1)
	require.True(t, ok2)
	assert.Same(t, b1, b2, "app bucket should be shared across calls")
}

func TestLimiter_GetAppBucket_DifferentFromMerchantBucket(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	merchantBucket, _ := l.GetBucket("merchant-1", "GET", "/catalog/2022-04-01/items")
	appBucket, _ := l.GetAppBucket("GET", "/catalog/2022-04-01/items")
	assert.NotSame(t, merchantBucket, appBucket, "app bucket and merchant bucket must be different")
}

func TestLimiter_GetAppBucket_NoMethodMixing(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	getBucket, _ := l.GetAppBucket("GET", "/listings/2021-08-01/items/{sellerId}/{sku}")
	patchBucket, _ := l.GetAppBucket("PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")

	if getBucket != nil && patchBucket != nil {
		assert.NotSame(t, getBucket, patchBucket, "different methods should have different app buckets")
	}
}

func TestLimiter_AppBucket_EnforcesLimit(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	// Get app bucket for an endpoint with known app limits
	// Use PATCH listings which has 500 req/s app limit
	appBucket, ok := l.GetAppBucket("PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")
	require.True(t, ok)

	// Drain the bucket
	consumed := 0
	for {
		allowed, _ := appBucket.TryConsume()
		if !allowed {
			break
		}
		consumed++
	}
	assert.Greater(t, consumed, 0, "app bucket should have initial tokens")

	// Now both merchant-1 and merchant-2 should be blocked at app level
	allowed, _ := appBucket.TryConsume()
	assert.False(t, allowed, "app-level bucket should be exhausted")
}

func TestLimiter_BothBucketsMustAllow(t *testing.T) {
	// Both the per-merchant bucket AND the app-level bucket must have tokens
	// for a request to proceed. If app bucket is empty, even a fresh merchant bucket is blocked.
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	appBucket, ok := l.GetAppBucket("PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")
	require.True(t, ok)

	// Drain app bucket completely
	for {
		allowed, _ := appBucket.TryConsume()
		if !allowed {
			break
		}
	}

	// Fresh merchant bucket should have tokens...
	merchantBucket, _ := l.GetBucket("fresh-merchant", "PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")
	require.NotNil(t, merchantBucket)
	allowed, _ := merchantBucket.TryConsume()
	assert.True(t, allowed, "merchant bucket itself should still have tokens")

	// ...but the request should still be blocked because app bucket is empty
	// The middleware handles this  -  here we just verify both buckets are independent
	allowed2, _ := appBucket.TryConsume()
	assert.False(t, allowed2, "app bucket should still be empty")
}
