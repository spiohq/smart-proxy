package cache

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/stretchr/testify/assert"
)

// Internal tests for resolveTTL and sanitizeCacheKeySuffix. The public
// CacheMiddleware behavior is exercised in middleware_test.go (package
// cache_test); these isolate the policy decisions.
//
// Pentest finding F-05.

func TestResolveTTL_ClampsClientHeaderToMaxClientTTL(t *testing.T) {
	cfg := &config.CacheConfig{MaxClientTTL: "24h"}
	tier := TierConfig{DefaultTTL: 60 * time.Second}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Cache-TTL", "8760h") // 1 year

	got := resolveTTL(r, tier, cfg)
	assert.Equal(t, 24*time.Hour, got, "client TTL must be clamped to MaxClientTTL")
}

func TestResolveTTL_ShorterClientHeaderIsHonored(t *testing.T) {
	cfg := &config.CacheConfig{MaxClientTTL: "24h"}
	tier := TierConfig{DefaultTTL: 60 * time.Second}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Cache-TTL", "30m")

	got := resolveTTL(r, tier, cfg)
	assert.Equal(t, 30*time.Minute, got)
}

func TestResolveTTL_NoClientHeader_ReturnsTierDefault(t *testing.T) {
	cfg := &config.CacheConfig{MaxClientTTL: "24h"}
	tier := TierConfig{DefaultTTL: 60 * time.Second}
	r := httptest.NewRequest("GET", "/", nil)

	got := resolveTTL(r, tier, cfg)
	assert.Equal(t, 60*time.Second, got)
}

func TestResolveTTL_TierDefaultIsNotClamped(t *testing.T) {
	// Operators trust their own tier defaults. The clamp only applies to
	// client-supplied TTL.
	cfg := &config.CacheConfig{MaxClientTTL: "1h"}
	tier := TierConfig{DefaultTTL: 7 * 24 * time.Hour} // a week
	r := httptest.NewRequest("GET", "/", nil)

	got := resolveTTL(r, tier, cfg)
	assert.Equal(t, 7*24*time.Hour, got, "tier default is operator-trusted; not clamped")
}

func TestResolveTTL_NilCfg_FallsBackTo24h(t *testing.T) {
	tier := TierConfig{DefaultTTL: 60 * time.Second}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-SP-Proxy-Cache-TTL", "999999h")

	// nil cfg should still clamp to 24h default.
	got := resolveTTL(r, tier, nil)
	assert.Equal(t, 24*time.Hour, got)
}

func TestSanitizeCacheKeySuffix_DropsBogus(t *testing.T) {
	// Spaces and semicolons are stripped; hyphens are part of the
	// allowed [A-Za-z0-9_-] alphabet and survive.
	got := sanitizeCacheKeySuffix("abc;DROP TABLE x;--")
	assert.Equal(t, "abcDROPTABLEx--", got, "non-allowed chars must be stripped; hyphens kept")
}

func TestSanitizeCacheKeySuffix_AcceptsAllowedChars(t *testing.T) {
	got := sanitizeCacheKeySuffix("abc-123_XYZ")
	assert.Equal(t, "abc-123_XYZ", got)
}

func TestSanitizeCacheKeySuffix_TruncatesAt64(t *testing.T) {
	got := sanitizeCacheKeySuffix(strings.Repeat("a", 100))
	assert.Equal(t, 64, len(got))
}

func TestSanitizeCacheKeySuffix_Empty(t *testing.T) {
	assert.Empty(t, sanitizeCacheKeySuffix(""))
}
