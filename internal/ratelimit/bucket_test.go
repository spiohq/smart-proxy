package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTokenBucket_StartsFullAtBurstCapacity(t *testing.T) {
	b := NewTokenBucket(1.0, 5.0, 0.8)
	for i := 0; i < 5; i++ {
		allowed, _ := b.TryConsume()
		assert.True(t, allowed, "token %d should be allowed", i+1)
	}
	allowed, wait := b.TryConsume()
	assert.False(t, allowed)
	assert.True(t, wait > 0)
}

func TestTokenBucket_EffectiveRate_AppliesThrottle(t *testing.T) {
	b := NewTokenBucket(10.0, 10.0, 0.8)
	assert.InDelta(t, 8.0, b.effectiveRate(), 0.001)
}

func TestTokenBucket_EffectiveRate_FullThrottle(t *testing.T) {
	b := NewTokenBucket(10.0, 10.0, 1.0)
	assert.InDelta(t, 10.0, b.effectiveRate(), 0.001)
}

func TestTokenBucket_WaitTime_CalculatedFromEffectiveRate(t *testing.T) {
	b := NewTokenBucket(10.0, 1.0, 0.8)
	allowed, _ := b.TryConsume()
	require.True(t, allowed)

	allowed, wait := b.TryConsume()
	assert.False(t, allowed)
	assert.InDelta(t, 125*time.Millisecond, wait, float64(20*time.Millisecond))
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	b := NewTokenBucket(100.0, 5.0, 1.0)
	for i := 0; i < 5; i++ {
		b.TryConsume()
	}
	time.Sleep(60 * time.Millisecond)
	allowed, _ := b.TryConsume()
	assert.True(t, allowed)
}

func TestTokenBucket_UpdateRate(t *testing.T) {
	b := NewTokenBucket(1.0, 5.0, 0.8)
	assert.InDelta(t, 0.8, b.effectiveRate(), 0.001)
	b.UpdateRate(2.0)
	assert.InDelta(t, 1.6, b.effectiveRate(), 0.001)
}

func TestTokenBucket_TokensNeverExceedMax(t *testing.T) {
	b := NewTokenBucket(1000.0, 3.0, 1.0)
	time.Sleep(50 * time.Millisecond)
	count := 0
	for {
		allowed, _ := b.TryConsume()
		if !allowed {
			break
		}
		count++
		if count > 10 {
			break
		}
	}
	assert.Equal(t, 3, count)
}

func TestTokenBucket_ConcurrentAccess(t *testing.T) {
	b := NewTokenBucket(1000.0, 100.0, 1.0)
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				b.TryConsume()
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
