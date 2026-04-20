package ratelimit

import (
	"sync"
	"time"
)

const (
	defaultFeedWindow    = 5 * time.Minute
	defaultFeedMaxPerWin = 5
	jsonListingsFeedType = "JSON_LISTINGS_FEED"
)

// FeedLimiter enforces special rate limits for JSON_LISTINGS_FEED submissions.
// Amazon limits this feed type to 5 submissions per 5 minutes per seller
// account per region, separate from the general createFeed rate limit.
type FeedLimiter struct {
	mu        sync.Mutex
	window    time.Duration
	maxPerWin int
	entries   map[string][]time.Time // key: "merchantKey:region"
}

// NewFeedLimiter creates a FeedLimiter with the standard 5-minute window.
func NewFeedLimiter() *FeedLimiter {
	return NewFeedLimiterWithWindow(defaultFeedWindow)
}

// NewFeedLimiterWithWindow creates a FeedLimiter with a custom window (useful for tests).
func NewFeedLimiterWithWindow(window time.Duration) *FeedLimiter {
	return &FeedLimiter{
		window:    window,
		maxPerWin: defaultFeedMaxPerWin,
		entries:   make(map[string][]time.Time),
	}
}

// Allow checks if a feed submission is allowed and records it if so.
// Only JSON_LISTINGS_FEED is subject to this limiter  -  other feed types always return true.
func (fl *FeedLimiter) Allow(merchantKey, region, feedType string) bool {
	if feedType != jsonListingsFeedType {
		return true
	}

	fl.mu.Lock()
	defer fl.mu.Unlock()

	key := merchantKey + ":" + region
	now := time.Now()
	cutoff := now.Add(-fl.window)

	// Prune expired entries
	timestamps := fl.entries[key]
	valid := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= fl.maxPerWin {
		fl.entries[key] = valid
		return false
	}

	fl.entries[key] = append(valid, now)
	return true
}

// IsJSONListingsFeed returns true if the feed type is JSON_LISTINGS_FEED.
func IsJSONListingsFeed(feedType string) bool {
	return feedType == jsonListingsFeedType
}
