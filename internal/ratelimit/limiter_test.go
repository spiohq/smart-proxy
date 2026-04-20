package ratelimit

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimiter_GetBucket_KnownEndpoint(t *testing.T) {
	l := NewLimiter(0.8, 100)
	defer l.Stop()
	bucket, known := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	require.True(t, known)
	require.NotNil(t, bucket)
}

func TestLimiter_GetBucket_UnknownEndpoint(t *testing.T) {
	l := NewLimiter(0.8, 100)
	defer l.Stop()
	_, known := l.GetBucket("merchant-1", "GET", "/unknown/v1/endpoint")
	assert.False(t, known)
}

func TestLimiter_GetBucket_SameMerchantEndpoint_ReturnsSameBucket(t *testing.T) {
	l := NewLimiter(0.8, 100)
	defer l.Stop()
	b1, _ := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	b2, _ := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	assert.Same(t, b1, b2)
}

func TestLimiter_GetBucket_DifferentMerchants_DifferentBuckets(t *testing.T) {
	l := NewLimiter(0.8, 100)
	defer l.Stop()
	b1, _ := l.GetBucket("merchant-1", "GET", "/orders/v0/orders")
	b2, _ := l.GetBucket("merchant-2", "GET", "/orders/v0/orders")
	assert.NotSame(t, b1, b2)
}

func TestLimiter_EnqueueAndWait_Success(t *testing.T) {
	l := NewLimiter(1.0, 10)
	defer l.Stop()
	bucket, _ := l.GetBucket("m1", "GET", "/orders/v0/orders")
	for i := 0; i < 19; i++ {
		bucket.TryConsume()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequest("GET", "/orders/v0/orders", nil)
	err := l.EnqueueAndWait(ctx, "m1", "GET", "/orders/v0/orders", req)
	require.NoError(t, err)
}

func TestLimiter_EnqueueAndWait_ContextCancellation(t *testing.T) {
	l := NewLimiter(0.8, 10)
	defer l.Stop()
	bucket, _ := l.GetBucket("m1", "GET", "/catalog/2022-04-01/items")
	for {
		allowed, _ := bucket.TryConsume()
		if !allowed {
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequest("GET", "/catalog/2022-04-01/items", nil)
	err := l.EnqueueAndWait(ctx, "m1", "GET", "/catalog/2022-04-01/items", req)
	assert.Error(t, err)
}

func TestLimiter_EnqueueAndWait_PriorityFromHeader(t *testing.T) {
	l := NewLimiter(1.0, 10)
	defer l.Stop()
	l.GetBucket("m1", "GET", "/orders/v0/orders")

	req, _ := http.NewRequest("GET", "/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Priority", "high")

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := l.EnqueueAndWait(ctx, "m1", "GET", "/orders/v0/orders", req)
	require.NoError(t, err)
}

func TestLimiter_UpdateFromResponse(t *testing.T) {
	l := NewLimiter(0.8, 100)
	defer l.Stop()
	l.GetBucket("m1", "GET", "/orders/v0/orders")

	resp := &http.Response{
		Header: http.Header{
			"X-Amzn-Ratelimit-Limit": {"0.5"},
		},
	}
	l.UpdateFromResponse("m1", "GET", "/orders/v0/orders", resp)

	bucket, _ := l.GetBucket("m1", "GET", "/orders/v0/orders")
	assert.InDelta(t, 0.4, bucket.effectiveRate(), 0.001)
}

func TestLimiter_UpdateFromResponse_NoHeader(t *testing.T) {
	l := NewLimiter(0.8, 100)
	defer l.Stop()
	l.GetBucket("m1", "GET", "/orders/v0/orders")

	resp := &http.Response{Header: http.Header{}}
	l.UpdateFromResponse("m1", "GET", "/orders/v0/orders", resp)
}

func TestLimiter_Stop_CleansUp(t *testing.T) {
	l := NewLimiter(0.8, 10)
	l.GetBucket("m1", "GET", "/orders/v0/orders")
	l.Stop()
}
