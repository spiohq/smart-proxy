package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── JSON_LISTINGS_FEED Special Rate Limiting Tests ────────────────────────────
// JSON_LISTINGS_FEED has a special rate limit: 5 submissions per 5 minutes
// per seller account per region, separate from the general createFeed limit.

func TestFeedLimiter_New(t *testing.T) {
	fl := NewFeedLimiter()
	require.NotNil(t, fl)
}

func TestFeedLimiter_AllowsUpTo5Submissions(t *testing.T) {
	fl := NewFeedLimiter()

	// 5 submissions should all be allowed
	for i := 0; i < 5; i++ {
		allowed := fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
		assert.True(t, allowed, "submission %d should be allowed", i+1)
	}
}

func TestFeedLimiter_Blocks6thSubmission(t *testing.T) {
	fl := NewFeedLimiter()

	// 5 allowed
	for i := 0; i < 5; i++ {
		fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	}

	// 6th should be blocked
	allowed := fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	assert.False(t, allowed, "6th JSON_LISTINGS_FEED submission should be blocked")
}

func TestFeedLimiter_DifferentMerchants_IndependentLimits(t *testing.T) {
	fl := NewFeedLimiter()

	// Exhaust merchant-1
	for i := 0; i < 5; i++ {
		fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	}

	// merchant-2 should still be allowed
	allowed := fl.Allow("merchant-2", "us-east-1", "JSON_LISTINGS_FEED")
	assert.True(t, allowed, "different merchant should have independent limit")
}

func TestFeedLimiter_DifferentRegions_IndependentLimits(t *testing.T) {
	fl := NewFeedLimiter()

	// Exhaust us-east-1
	for i := 0; i < 5; i++ {
		fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	}

	// eu-west-1 should still be allowed
	allowed := fl.Allow("merchant-1", "eu-west-1", "JSON_LISTINGS_FEED")
	assert.True(t, allowed, "different region should have independent limit")
}

func TestFeedLimiter_NonJSONListingsFeed_NotLimited(t *testing.T) {
	fl := NewFeedLimiter()

	// Other feed types should not be limited by the JSON_LISTINGS_FEED limiter
	for i := 0; i < 20; i++ {
		allowed := fl.Allow("merchant-1", "us-east-1", "POST_FLAT_FILE_LISTINGS_DATA")
		assert.True(t, allowed, "non-JSON_LISTINGS_FEED should not be limited")
	}
}

func TestFeedLimiter_WindowExpiry(t *testing.T) {
	fl := NewFeedLimiterWithWindow(100 * time.Millisecond) // short window for test

	// Exhaust the limit
	for i := 0; i < 5; i++ {
		fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	}
	assert.False(t, fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED"))

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	allowed := fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	assert.True(t, allowed, "should be allowed after window expires")
}

func TestFeedLimiter_SlidingWindow(t *testing.T) {
	fl := NewFeedLimiterWithWindow(200 * time.Millisecond)

	// Submit 3 at t=0
	for i := 0; i < 3; i++ {
		fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED")
	}

	// Wait 120ms, then submit 2 more (at t=120ms)
	time.Sleep(120 * time.Millisecond)
	assert.True(t, fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED"))
	assert.True(t, fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED"))

	// 6th should be blocked (5 in the window)
	assert.False(t, fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED"))

	// Wait another 100ms (t=220ms)  -  the first 3 submissions should have expired
	time.Sleep(100 * time.Millisecond)
	assert.True(t, fl.Allow("merchant-1", "us-east-1", "JSON_LISTINGS_FEED"),
		"should be allowed after early submissions expire")
}

func TestIsJSONListingsFeed_True(t *testing.T) {
	assert.True(t, IsJSONListingsFeed("JSON_LISTINGS_FEED"))
}

func TestIsJSONListingsFeed_False(t *testing.T) {
	assert.False(t, IsJSONListingsFeed("POST_FLAT_FILE_LISTINGS_DATA"))
	assert.False(t, IsJSONListingsFeed(""))
	assert.False(t, IsJSONListingsFeed("json_listings_feed")) // case-sensitive
}
