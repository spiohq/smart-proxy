package ratelimit

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type bucketEntry struct {
	bucket   *TokenBucket
	queue    *RequestQueue
	lastUsed atomic.Int64 // UnixNano timestamp
}

type Limiter struct {
	buckets       sync.Map // key: "merchantKey:METHOD:endpoint"
	appBuckets    sync.Map // key: "METHOD:endpoint" (cross-merchant)
	throttle      float64
	queueMaxDepth int
	done          chan struct{}
}

func NewLimiter(throttle float64, queueMaxDepth int) *Limiter {
	return &Limiter{
		throttle:      throttle,
		queueMaxDepth: queueMaxDepth,
		done:          make(chan struct{}),
	}
}

func (l *Limiter) GetBucket(merchantKey, method, endpoint string) (*TokenBucket, bool) {
	key := merchantKey + ":" + method + ":" + endpoint
	if val, ok := l.buckets.Load(key); ok {
		entry := val.(*bucketEntry)
		entry.lastUsed.Store(time.Now().UnixNano())
		return entry.bucket, true
	}

	params, known := LookupDefaults(method, endpoint)
	if !known {
		return nil, false
	}

	bucket := NewTokenBucket(params.Rate, params.Burst, l.throttle)
	queue := NewRequestQueue(bucket, l.queueMaxDepth)
	entry := &bucketEntry{
		bucket: bucket,
		queue:  queue,
	}
	entry.lastUsed.Store(time.Now().UnixNano())
	actual, _ := l.buckets.LoadOrStore(key, entry)
	actualEntry := actual.(*bucketEntry)
	if actualEntry != entry {
		queue.Stop()
	}
	return actualEntry.bucket, true
}

// GetAppBucket returns the application-level (cross-merchant) bucket for the
// given method and endpoint. Returns nil, false if no app-level limit is defined.
func (l *Limiter) GetAppBucket(method, endpoint string) (*TokenBucket, bool) {
	key := method + ":" + endpoint
	if val, ok := l.appBuckets.Load(key); ok {
		entry := val.(*bucketEntry)
		entry.lastUsed.Store(time.Now().UnixNano())
		return entry.bucket, true
	}

	params, ok := AppBucketParams[key]
	if !ok {
		return nil, false
	}

	// App buckets always use throttle=1.0 (not affected by per-merchant throttle)
	bucket := NewTokenBucket(params.Rate, params.Burst, 1.0)
	queue := NewRequestQueue(bucket, l.queueMaxDepth)
	entry := &bucketEntry{
		bucket: bucket,
		queue:  queue,
	}
	entry.lastUsed.Store(time.Now().UnixNano())
	actual, _ := l.appBuckets.LoadOrStore(key, entry)
	actualEntry := actual.(*bucketEntry)
	if actualEntry != entry {
		queue.Stop()
	}
	return actualEntry.bucket, true
}

func (l *Limiter) getQueue(merchantKey, method, endpoint string) *RequestQueue {
	key := merchantKey + ":" + method + ":" + endpoint
	if val, ok := l.buckets.Load(key); ok {
		return val.(*bucketEntry).queue
	}
	return nil
}

func (l *Limiter) EnqueueAndWait(ctx context.Context, merchantKey, method, endpoint string, r *http.Request) error {
	q := l.getQueue(merchantKey, method, endpoint)
	if q == nil {
		return ErrQueueFull
	}
	priority := resolvePriority(r)
	return q.Enqueue(ctx, priority)
}

func resolvePriority(r *http.Request) PriorityLevel {
	switch strings.ToLower(r.Header.Get("X-SP-Proxy-Priority")) {
	case "high":
		return PriorityHigh
	case "low":
		return PriorityLow
	default:
		return PriorityNormal
	}
}

func (l *Limiter) UpdateFromResponse(merchantKey, method, endpoint string, resp *http.Response) {
	rateLimitHeader := resp.Header.Get("x-amzn-RateLimit-Limit")
	if rateLimitHeader == "" {
		return
	}
	rate, err := strconv.ParseFloat(rateLimitHeader, 64)
	if err != nil {
		return
	}
	bucket, known := l.GetBucket(merchantKey, method, endpoint)
	if !known {
		return
	}
	bucket.UpdateRate(rate)
}

func (l *Limiter) StartGC(ttl time.Duration) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				l.gc(ttl)
			case <-l.done:
				return
			}
		}
	}()
}

func (l *Limiter) gc(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl)
	gcMap := func(m *sync.Map) {
		m.Range(func(key, value any) bool {
			entry := value.(*bucketEntry)
			if time.Unix(0, entry.lastUsed.Load()).Before(cutoff) {
				entry.queue.Stop()
				m.Delete(key)
			}
			return true
		})
	}
	gcMap(&l.buckets)
	gcMap(&l.appBuckets)
}

// BucketCount returns the number of active per-merchant rate limit buckets.
func (l *Limiter) BucketCount() int {
	count := 0
	l.buckets.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (l *Limiter) Stop() {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	stopAll := func(m *sync.Map) {
		m.Range(func(key, value any) bool {
			value.(*bucketEntry).queue.Stop()
			return true
		})
	}
	stopAll(&l.buckets)
	stopAll(&l.appBuckets)
}
