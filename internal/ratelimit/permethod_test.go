package ratelimit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Per-Method Rate Limiting Tests ────────────────────────────────────────────
// These tests verify that rate limits are per HTTP method + endpoint, not just
// per endpoint. E.g., GET and PATCH on the same path get different buckets.

func TestLimiter_GetBucket_DifferentMethods_DifferentBuckets(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	// GET and PATCH on the same listings endpoint should get separate buckets
	getBucket, getKnown := l.GetBucket("merchant-1", "GET", "/listings/2021-08-01/items/{sellerId}/{sku}")
	patchBucket, patchKnown := l.GetBucket("merchant-1", "PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")

	require.True(t, getKnown)
	require.True(t, patchKnown)
	assert.NotSame(t, getBucket, patchBucket, "GET and PATCH should have separate buckets")
}

func TestLimiter_GetBucket_SameMethodSameEndpoint_SameBucket(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	b1, _ := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	b2, _ := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	assert.Same(t, b1, b2)
}

func TestLimiter_GetBucket_DifferentMerchantsSameMethod_DifferentBuckets(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	b1, _ := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	b2, _ := l.GetBucket("merchant-2", "GET", "/orders/v0/orders")
	assert.NotSame(t, b1, b2)
}

func TestLookupDefaults_PerMethod_FeedsAPI(t *testing.T) {
	// GET /feeds: Rate 0.0222, Burst 10 (getFeeds)
	// POST /feeds: Rate 0.0083, Burst 15 (createFeed  -  method override)
	ep := "/feeds/2021-06-30/feeds"

	getParams, ok := LookupDefaults("GET", ep)
	require.True(t, ok)
	assert.InDelta(t, 0.0222, getParams.Rate, 0.0001)
	assert.InDelta(t, 10.0, getParams.Burst, 0.1)

	postParams, ok := LookupDefaults("POST", ep)
	require.True(t, ok)
	assert.InDelta(t, 0.0083, postParams.Rate, 0.0001)
	assert.InDelta(t, 15.0, postParams.Burst, 0.1)
}

func TestLookupDefaults_PerMethod_ReportsAPI(t *testing.T) {
	// GET /reports: Rate 0.0222, Burst 10 (getReports)
	// POST /reports: Rate 0.0167, Burst 15 (createReport  -  method override)
	ep := "/reports/2021-06-30/reports"

	getParams, ok := LookupDefaults("GET", ep)
	require.True(t, ok)
	assert.InDelta(t, 0.0222, getParams.Rate, 0.0001)

	postParams, ok := LookupDefaults("POST", ep)
	require.True(t, ok)
	assert.InDelta(t, 0.0167, postParams.Rate, 0.0001)
}

func TestLookupDefaults_MethodFallback(t *testing.T) {
	// Most endpoints have the same limits for all methods.
	// When no method-specific override exists, any method should fall back to the default.
	ep := "/orders/v0/orders"

	getParams, ok := LookupDefaults("GET", ep)
	require.True(t, ok)
	assert.InDelta(t, 0.0167, getParams.Rate, 0.0001)

	postParams, ok := LookupDefaults("POST", ep)
	require.True(t, ok)
	assert.InDelta(t, 0.0167, postParams.Rate, 0.0001)
	assert.InDelta(t, getParams.Burst, postParams.Burst, 0.1, "fallback should give same params")
}

func TestLookupDefaults_UnknownEndpoint_ReturnsFalse(t *testing.T) {
	_, ok := LookupDefaults("GET", "/unknown/v1/endpoint")
	assert.False(t, ok)
}

func TestLimiter_GetBucket_MethodInBucketKey(t *testing.T) {
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	// Create GET bucket and drain it
	getBucket, _ := l.GetBucket("m1", "GET", "/listings/2021-08-01/items/{sellerId}/{sku}")
	for i := 0; i < 10; i++ {
		getBucket.TryConsume()
	}

	// PATCH bucket should still be full (separate bucket)
	patchBucket, _ := l.GetBucket("m1", "PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")
	allowed, _ := patchBucket.TryConsume()
	assert.True(t, allowed, "PATCH bucket should be independent of GET bucket")
}

func TestLimiter_GetBucket_ListingsItems_PairLevel_SameBurst(t *testing.T) {
	// At the pair level, ALL methods on Listings Items have Burst 5.
	// The differentiation happens at the app level, not pair level.
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	getBucket, _ := l.GetBucket("m1", "GET", "/listings/2021-08-01/items/{sellerId}/{sku}")
	patchBucket, _ := l.GetBucket("m1", "PATCH", "/listings/2021-08-01/items/{sellerId}/{sku}")

	getConsumed := 0
	for {
		allowed, _ := getBucket.TryConsume()
		if !allowed {
			break
		}
		getConsumed++
	}

	patchConsumed := 0
	for {
		allowed, _ := patchBucket.TryConsume()
		if !allowed {
			break
		}
		patchConsumed++
	}

	assert.Equal(t, 5, getConsumed, "GET bucket should have burst=5 tokens")
	assert.Equal(t, 5, patchConsumed, "PATCH bucket should also have burst=5 tokens at pair level")
}

func TestLimiter_GetBucket_FeedsAPI_DifferentBurstPerMethod(t *testing.T) {
	// Feeds: GET has Burst 10, POST has Burst 15 (from method override)
	l := NewLimiter(1.0, 100)
	defer l.Stop()

	getBucket, _ := l.GetBucket("m1", "GET", "/feeds/2021-06-30/feeds")
	postBucket, _ := l.GetBucket("m1", "POST", "/feeds/2021-06-30/feeds")

	getConsumed := 0
	for {
		allowed, _ := getBucket.TryConsume()
		if !allowed {
			break
		}
		getConsumed++
	}

	postConsumed := 0
	for {
		allowed, _ := postBucket.TryConsume()
		if !allowed {
			break
		}
		postConsumed++
	}

	assert.Equal(t, 10, getConsumed, "GET /feeds should have burst=10")
	assert.Equal(t, 15, postConsumed, "POST /feeds (createFeed) should have burst=15")
}
