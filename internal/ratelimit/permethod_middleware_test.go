package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
	"github.com/stretchr/testify/assert"
)

func withMerchantHelper(next http.Handler, merchantKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := merchant.ResolvedMerchant{Key: merchantKey, Source: "test"}
		ctx := merchant.ContextWithMerchant(r.Context(), m)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestMiddleware_PerMethod_GETAndPATCH_IndependentBuckets(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 100)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler := withMerchantHelper(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), "test-merchant")

	// Drain GET bucket for listings endpoint (burst=10)
	for i := 0; i < 15; i++ {
		req := httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER1/SKU1", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// GET should be rate-limited now
	getReq := httptest.NewRequest("GET", "/listings/2021-08-01/items/SELLER1/SKU1", nil)
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)
	assert.Equal(t, 429, getW.Code, "GET should be rate limited after draining")

	// PATCH should still be allowed (separate bucket)
	patchReq := httptest.NewRequest("PATCH", "/listings/2021-08-01/items/SELLER1/SKU1", nil)
	patchW := httptest.NewRecorder()
	handler.ServeHTTP(patchW, patchReq)
	assert.Equal(t, 200, patchW.Code, "PATCH should still have tokens in its own bucket")
}

func TestMiddleware_PerMethod_AppLevelEnforced(t *testing.T) {
	limiter := ratelimit.NewLimiter(1.0, 100)
	defer limiter.Stop()
	cfg := &config.RateLimitConfig{
		Enabled:     true,
		DefaultMode: "reject",
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	// Use many different merchants to exhaust the app-level bucket
	// The app-level limit is shared across all merchants
	requestsMade := 0
	rejections := 0
	for i := 0; i < 600; i++ {
		merchantKey := "merchant-" + string(rune('A'+i%26))
		handler := withMerchantHelper(ratelimit.RateLimitMiddleware(limiter, cfg)(inner), merchantKey)

		req := httptest.NewRequest("PATCH", "/listings/2021-08-01/items/SELLER1/SKU1", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		requestsMade++
		if w.Code == 429 {
			rejections++
		}
	}

	// With app-level limit of 500 for PATCH listings, we should see some rejections
	assert.Greater(t, rejections, 0, "app-level rate limit should cause some rejections when many merchants hit the same endpoint")
}
