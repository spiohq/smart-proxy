package config

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()
	assert.Equal(t, 8080, cfg.Server.PortEU)
	assert.Equal(t, true, cfg.RateLimit.Enabled)
	assert.Equal(t, "queue", cfg.RateLimit.DefaultMode)
	assert.Equal(t, "60s", cfg.RateLimit.QueueTimeout)
	assert.Equal(t, 100, cfg.RateLimit.QueueMaxDepth)
	assert.InDelta(t, 0.8, cfg.RateLimit.ThrottleFactor, 0.001)
	assert.Equal(t, "2h", cfg.RateLimit.BucketTTL)
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("SP_PROXY_RATELIMIT_MODE", "reject")
	os.Setenv("SP_PROXY_RATELIMIT_THROTTLE_FACTOR", "0.5")
	defer os.Unsetenv("SP_PROXY_RATELIMIT_MODE")
	defer os.Unsetenv("SP_PROXY_RATELIMIT_THROTTLE_FACTOR")

	cfg := Load()
	assert.Equal(t, "reject", cfg.RateLimit.DefaultMode)
	assert.InDelta(t, 0.5, cfg.RateLimit.ThrottleFactor, 0.001)
}

func TestValidate_AtLeastOnePort(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{PortEU: 0, PortNA: 0, PortFE: 0, PortDashboard: 9090},
	}
	err := cfg.Validate()
	require.Error(t, err)
}

func TestEnvBool(t *testing.T) {
	assert.True(t, envBool("NONEXISTENT_VAR", true))
	os.Setenv("TEST_BOOL", "false")
	defer os.Unsetenv("TEST_BOOL")
	assert.False(t, envBool("TEST_BOOL", true))
}

func TestLoad_CacheDefaults(t *testing.T) {
	cfg := Load()
	assert.True(t, cfg.Cache.Enabled)
	assert.Equal(t, int64(268435456), cfg.Cache.MaxMemory)
	assert.Equal(t, "60s", cfg.Cache.DefaultTTL)
	assert.True(t, cfg.Cache.ExcludePII)
}

func TestLoad_CacheEnvOverrides(t *testing.T) {
	os.Setenv("SP_PROXY_CACHE_ENABLED", "false")
	os.Setenv("SP_PROXY_CACHE_MAX_MEMORY", "134217728")
	os.Setenv("SP_PROXY_CACHE_DEFAULT_TTL", "120s")
	os.Setenv("SP_PROXY_CACHE_EXCLUDE_PII", "false")
	defer os.Unsetenv("SP_PROXY_CACHE_ENABLED")
	defer os.Unsetenv("SP_PROXY_CACHE_MAX_MEMORY")
	defer os.Unsetenv("SP_PROXY_CACHE_DEFAULT_TTL")
	defer os.Unsetenv("SP_PROXY_CACHE_EXCLUDE_PII")

	cfg := Load()
	assert.False(t, cfg.Cache.Enabled)
	assert.Equal(t, int64(134217728), cfg.Cache.MaxMemory)
	assert.Equal(t, "120s", cfg.Cache.DefaultTTL)
	assert.False(t, cfg.Cache.ExcludePII)
}

func TestValidate_CacheDefaultTTL(t *testing.T) {
	cfg := Load()
	cfg.Cache.Enabled = true
	cfg.Cache.DefaultTTL = "invalid"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache default TTL")
}

func TestValidate_CacheMaxMemory(t *testing.T) {
	cfg := Load()
	cfg.Cache.Enabled = true
	cfg.Cache.MaxMemory = 0
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache max memory")
}

func TestValidate_NegativeDuration(t *testing.T) {
	cfg := Load()
	cfg.RateLimit.BucketTTL = "-1h"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestValidate_QueueMaxDepthZero(t *testing.T) {
	cfg := Load()
	cfg.RateLimit.QueueMaxDepth = 0
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue max depth")
}

func TestValidate_NegativePurgeDuration(t *testing.T) {
	cfg := Load()
	cfg.Purge.MetadataRetention = "-24h"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestValidate_NegativeCacheTTL(t *testing.T) {
	cfg := Load()
	cfg.Cache.Enabled = true
	cfg.Cache.DefaultTTL = "-60s"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestLoadWithLogger_InvalidEnvLogged(t *testing.T) {
	os.Setenv("SP_PROXY_PORT_EU", "notanumber")
	defer os.Unsetenv("SP_PROXY_PORT_EU")

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := LoadWithLogger(logger)

	assert.Equal(t, 8080, cfg.Server.PortEU)
	assert.Contains(t, buf.String(), "SP_PROXY_PORT_EU")
}

func TestValidate_SQLitePathParentMissing(t *testing.T) {
	cfg := Load()
	cfg.Storage.SQLitePath = "/nonexistent/dir/test.db"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SQLite path parent directory")
}

func TestValidate_SQLitePathMemoryOK(t *testing.T) {
	cfg := Load()
	cfg.Storage.SQLitePath = ":memory:"
	cfg.Bodies.Enabled = false
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestLoad_BodiesDefaults(t *testing.T) {
	cfg := Load()
	assert.True(t, cfg.Bodies.Enabled)
	assert.Equal(t, "/data/bodies", cfg.Bodies.BasePath)
	assert.Equal(t, "24h", cfg.Bodies.RecentMaxAge)
	assert.Equal(t, "720h", cfg.Bodies.ArchiveMaxAge)
	assert.Equal(t, "zstd", cfg.Bodies.Compression)
	assert.Equal(t, int64(256*1024), cfg.Bodies.MaxCaptureSize)
	assert.Equal(t, int64(8*1024*1024*1024), cfg.Bodies.MaxBytes)
}

func TestValidate_BodiesMaxBytesNegative(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.MaxBytes = -1
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MAX_BYTES")
}

func TestValidate_BodiesMaxCaptureSize(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.MaxCaptureSize = 0
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MAX_CAPTURE_SIZE")
}

func TestValidate_BodiesCompressionUnknown(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.Compression = "lz4"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "COMPRESSION")
}
