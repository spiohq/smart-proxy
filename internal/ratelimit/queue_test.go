package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestQueue_EnqueueAndDequeue_FIFO(t *testing.T) {
	bucket := NewTokenBucket(100.0, 0.0, 1.0) // Start empty, fast refill
	q := NewRequestQueue(bucket, 10)
	defer q.Stop()

	ctx := context.Background()
	start := time.Now()
	err := q.Enqueue(ctx, PriorityNormal)
	require.NoError(t, err)

	assert.Less(t, time.Since(start), 200*time.Millisecond)
}

func TestRequestQueue_RespectsMaxDepth(t *testing.T) {
	bucket := NewTokenBucket(0.001, 0.0, 1.0) // Very slow
	q := NewRequestQueue(bucket, 2)
	defer q.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 3)
	for i := 0; i < 2; i++ {
		go func() { errCh <- q.Enqueue(ctx, PriorityNormal) }()
	}
	time.Sleep(50 * time.Millisecond)

	err := q.Enqueue(ctx, PriorityNormal)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queue full")
}

func TestRequestQueue_ContextCancellation(t *testing.T) {
	bucket := NewTokenBucket(0.001, 0.0, 1.0)
	q := NewRequestQueue(bucket, 10)
	defer q.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := q.Enqueue(ctx, PriorityNormal)
	assert.Error(t, err)
}

func TestRequestQueue_PriorityOrdering(t *testing.T) {
	bucket := NewTokenBucket(0.001, 0.0, 1.0)
	q := NewRequestQueue(bucket, 10)
	defer q.Stop()

	order := make(chan PriorityLevel, 3)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		q.Enqueue(ctx, PriorityLow)
		order <- PriorityLow
	}()
	time.Sleep(10 * time.Millisecond)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		q.Enqueue(ctx, PriorityHigh)
		order <- PriorityHigh
	}()
	time.Sleep(10 * time.Millisecond)

	bucket.UpdateRate(1000.0)

	first := <-order
	assert.Equal(t, PriorityHigh, first)
}

func TestRequestQueue_Stop_UnblocksWaiters(t *testing.T) {
	bucket := NewTokenBucket(0.001, 0.0, 1.0)
	q := NewRequestQueue(bucket, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- q.Enqueue(context.Background(), PriorityNormal)
	}()

	time.Sleep(50 * time.Millisecond)
	q.Stop()

	err := <-errCh
	assert.Error(t, err)
}
