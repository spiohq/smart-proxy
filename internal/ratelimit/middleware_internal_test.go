package ratelimit

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/stretchr/testify/assert"
)

// Internal tests for resolveTimeout / resolveThrottleMode -- the public
// behavior is exercised end-to-end in middleware_test.go (package
// ratelimit_test); these isolate the policy decisions.
//
// Pentest finding F-11: the X-SP-Proxy-Throttle-Mode header is advisory
// and must not let the caller exceed the operator's QueueTimeout ceiling
// or upgrade past an operator-chosen reject mode.

func TestResolveTimeout_ClampsHeaderToConfigCeiling(t *testing.T) {
	cfg := &config.RateLimitConfig{QueueTimeout: "60s"}
	r := httptest.NewRequest("GET", "/", nil)
	// Header tries to ask for ~27 hours.
	r.Header.Set("X-SP-Proxy-Throttle-Mode", "queue-timeout:99999999")

	got := resolveTimeout(r, cfg)
	assert.LessOrEqual(t, got, 60*time.Second,
		"header-supplied queue-timeout must be clamped to cfg.QueueTimeout (operator's ceiling)")
}

func TestResolveTimeout_HeaderShorter_Honored(t *testing.T) {
	cfg := &config.RateLimitConfig{QueueTimeout: "60s"}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Throttle-Mode", "queue-timeout:500")

	got := resolveTimeout(r, cfg)
	assert.Equal(t, 500*time.Millisecond, got,
		"header may shorten the wait below cfg.QueueTimeout")
}

func TestResolveTimeout_NoHeader_FallsBackToConfig(t *testing.T) {
	cfg := &config.RateLimitConfig{QueueTimeout: "30s"}
	r := httptest.NewRequest("GET", "/", nil)
	got := resolveTimeout(r, cfg)
	assert.Equal(t, 30*time.Second, got)
}

func TestResolveThrottleMode_RejectIsCeiling_HeaderCannotUpgradeToQueue(t *testing.T) {
	cfg := &config.RateLimitConfig{DefaultMode: "reject"}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Throttle-Mode", "queue")

	got := resolveThrottleMode(r, "merchant-a", "/orders", cfg)
	assert.Equal(t, ThrottleModeReject, got,
		"operator-chosen reject must be a ceiling; header cannot upgrade to queue")
}

func TestResolveThrottleMode_RejectIsCeiling_HeaderCanStayReject(t *testing.T) {
	cfg := &config.RateLimitConfig{DefaultMode: "reject"}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Throttle-Mode", "reject")

	got := resolveThrottleMode(r, "merchant-a", "/orders", cfg)
	assert.Equal(t, ThrottleModeReject, got)
}

func TestResolveThrottleMode_QueueDefault_HeaderCanDowngradeToReject(t *testing.T) {
	cfg := &config.RateLimitConfig{DefaultMode: "queue"}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Throttle-Mode", "reject")

	got := resolveThrottleMode(r, "merchant-a", "/orders", cfg)
	assert.Equal(t, ThrottleModeReject, got,
		"caller may downgrade to reject for fail-fast semantics")
}

func TestResolveThrottleMode_PerMerchantOverridesDefault(t *testing.T) {
	cfg := &config.RateLimitConfig{
		DefaultMode:   "queue",
		MerchantModes: map[string]string{"merchant-a": "reject"},
	}
	r := httptest.NewRequest("GET", "/", nil)

	got := resolveThrottleMode(r, "merchant-a", "/orders", cfg)
	assert.Equal(t, ThrottleModeReject, got)
}
