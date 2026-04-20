package ratelimit

import (
	"math"
	"sync"
	"time"
)

type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	throttle   float64
}

func NewTokenBucket(rate, burst, throttle float64) *TokenBucket {
	maxTokens := burst
	if maxTokens <= 0 {
		maxTokens = math.Max(rate, 1.0)
	}
	return &TokenBucket{
		tokens:     burst,
		maxTokens:  maxTokens,
		refillRate: rate,
		lastRefill: time.Now(),
		throttle:   throttle,
	}
}

func (b *TokenBucket) effectiveRate() float64 {
	r := b.refillRate * b.throttle
	if r <= 0 {
		return 1e-9
	}
	return r
}

func (b *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = math.Min(b.maxTokens, b.tokens+elapsed*b.effectiveRate())
	b.lastRefill = now
}

func (b *TokenBucket) TryConsume() (allowed bool, waitTime time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}
	deficit := 1.0 - b.tokens
	wait := time.Duration(deficit / b.effectiveRate() * float64(time.Second))
	return false, wait
}

func (b *TokenBucket) UpdateRate(newRate float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refillRate = newRate
}

func (b *TokenBucket) Tokens() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}
