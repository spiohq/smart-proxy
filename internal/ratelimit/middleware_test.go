package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withMerchant(next http.Handler, merchantKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := merchant.ResolvedMerchant{Key: merchantKey, Source: "test"}
		ctx := merchant.ContextWithMerchant(r.Context(), m)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestMiddleware_AllowsRequest_WhenTokenAvailable(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	})

	handler := withMerchant(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), "test-merchant")

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, 200, w.Code)
}

func TestMiddleware_RejectMode_Returns429(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler := withMerchant(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), "test-merchant")

	// Drain the bucket for /orders/v0/orders (burst=20)
	for i := 0; i < 25; i++ {
		req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Next request should be rejected
	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, 429, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestMiddleware_UnknownEndpoint_PassesThrough(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	})

	handler := withMerchant(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), "test-merchant")

	req := httptest.NewRequest("GET", "/unknown/v1/widgets", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "false", w.Header().Get("X-SP-Proxy-Rate-Limit-Active"))
}

func TestMiddleware_Disabled_PassesThrough(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled: false,
	}

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	})

	handler := withMerchant(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), "test-merchant")

	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, called)
	assert.Equal(t, 200, w.Code)
}

func TestMiddleware_HeaderOverride_ThrottleMode(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 10)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "queue",
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := withMerchant(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), "test-merchant")

	// Drain the bucket using reject mode to avoid blocking
	for i := 0; i < 25; i++ {
		req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
		req.Header.Set("X-SP-Proxy-Throttle-Mode", "reject")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Send with reject override header (verifying header override works)
	req := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Throttle-Mode", "reject")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, 429, w.Code)
}
