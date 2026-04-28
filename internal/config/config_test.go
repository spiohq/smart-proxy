package config

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
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

func TestPIIConfig_QueryParamsExtra_Default(t *testing.T) {
	t.Setenv("SP_PROXY_PII_QUERY_PARAMS", "")
	cfg := Load()
	assert.Empty(t, cfg.PII.QueryParamsExtra)
}

func TestPIIConfig_QueryParamsExtra_FromEnv(t *testing.T) {
	t.Setenv("SP_PROXY_PII_QUERY_PARAMS", "foo, bar ,baz")
	cfg := Load()
	assert.Equal(t, []string{"foo", "bar", "baz"}, cfg.PII.QueryParamsExtra)
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

func TestLoad_EnvDefaultsToDevelopment(t *testing.T) {
	os.Unsetenv("SP_PROXY_ENV")
	cfg := Load()
	assert.Equal(t, "development", cfg.Env)
	assert.False(t, cfg.IsProduction())
}

func TestLoad_EnvProduction(t *testing.T) {
	t.Setenv("SP_PROXY_ENV", "production")
	cfg := Load()
	assert.Equal(t, "production", cfg.Env)
	assert.True(t, cfg.IsProduction())
}

func TestIsProduction_CaseInsensitive(t *testing.T) {
	assert.True(t, (&Config{Env: "Production"}).IsProduction())
	assert.True(t, (&Config{Env: "PRODUCTION"}).IsProduction())
	assert.False(t, (&Config{Env: "prod"}).IsProduction())
	assert.False(t, (&Config{Env: ""}).IsProduction())
}

func TestLoad_PIIFailClosedDefault(t *testing.T) {
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "")
	cfg := Load()
	assert.True(t, cfg.PII.FailClosed, "fail-closed must default to true for DPP compliance")
}

func TestLoad_PIIFailClosedEnv(t *testing.T) {
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "true")
	cfg := Load()
	assert.True(t, cfg.PII.FailClosed)
}

func TestLoad_S3SSEDefaults(t *testing.T) {
	os.Unsetenv("SP_PROXY_S3_SSE")
	os.Unsetenv("SP_PROXY_S3_SSE_KMS_KEY")
	cfg := Load()
	assert.Equal(t, "", cfg.Bodies.S3.SSE)
	assert.Equal(t, "", cfg.Bodies.S3.SSEKMSKey)
}

func TestLoad_S3SSEEnvOverrides(t *testing.T) {
	t.Setenv("SP_PROXY_S3_SSE", "aws:kms")
	t.Setenv("SP_PROXY_S3_SSE_KMS_KEY", "alias/proxy-bodies")
	cfg := Load()
	assert.Equal(t, "aws:kms", cfg.Bodies.S3.SSE)
	assert.Equal(t, "alias/proxy-bodies", cfg.Bodies.S3.SSEKMSKey)
}

func TestValidate_S3SSEInvalid(t *testing.T) {
	cfg := validatableS3Cfg()
	cfg.Bodies.S3.SSE = "rot13"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SP_PROXY_S3_SSE")
}

func TestValidate_S3SSEAcceptsKnownValues(t *testing.T) {
	for _, sse := range []string{"", "AES256", "aws:kms", "aws:kms:dsse"} {
		cfg := validatableS3Cfg()
		cfg.Bodies.S3.SSE = sse
		assert.NoError(t, cfg.Validate(), "SSE=%q should validate", sse)
	}
}

// validatableS3Cfg returns a baseline config that is valid for S3 backend
// tests: in-memory SQLite, current-dir bodies path, EU port set so Validate
// does not trip over unrelated requirements.
func validatableS3Cfg() *Config {
	cfg := Load()
	cfg.Storage.SQLitePath = ":memory:"
	cfg.Bodies.BasePath = "."
	cfg.Bodies.Enabled = true
	cfg.Bodies.Backend = "s3"
	cfg.Bodies.S3.Bucket = "b"
	return cfg
}

func TestValidate_S3InsecureEndpointBlockedInProd(t *testing.T) {
	cfg := validatableS3Cfg()
	cfg.Env = "production"
	cfg.Bodies.S3.Endpoint = "http://minio.internal:9000"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plain http")
}

func TestValidate_S3InsecureEndpointAllowedInDev(t *testing.T) {
	cfg := validatableS3Cfg()
	cfg.Env = "development"
	cfg.Bodies.S3.Endpoint = "http://minio.internal:9000"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_S3HTTPSEndpointAllowedInProd(t *testing.T) {
	cfg := validatableS3Cfg()
	cfg.Env = "production"
	cfg.Bodies.S3.Endpoint = "https://minio.internal:9000"
	cfg.Bodies.S3.SSE = "AES256"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_S3EmptyEndpointAllowedInProd(t *testing.T) {
	// Empty endpoint = real AWS = SDK uses https.
	cfg := validatableS3Cfg()
	cfg.Env = "production"
	cfg.Bodies.S3.Endpoint = ""
	cfg.Bodies.S3.SSE = "AES256"
	assert.NoError(t, cfg.Validate())
}

func TestWarnings_HTTPSEndpointAndExplicitSSE_NoWarnings(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.Backend = "s3"
	cfg.Bodies.S3.Bucket = "b"
	cfg.Bodies.S3.Endpoint = "https://s3.eu-central-1.amazonaws.com"
	cfg.Bodies.S3.SSE = "AES256"
	assert.Empty(t, cfg.Warnings())
}

func TestWarnings_PlainHTTPEndpointFlagged(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.Backend = "s3"
	cfg.Bodies.S3.Bucket = "b"
	cfg.Bodies.S3.Endpoint = "http://minio.internal:9000"
	cfg.Bodies.S3.SSE = "AES256"
	w := cfg.Warnings()
	require.Len(t, w, 1)
	assert.Contains(t, w[0], "plain http")
}

func TestWarnings_EmptySSEFlagged(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.Backend = "s3"
	cfg.Bodies.S3.Bucket = "b"
	cfg.Bodies.S3.Endpoint = "https://s3.amazonaws.com"
	cfg.Bodies.S3.SSE = ""
	w := cfg.Warnings()
	require.Len(t, w, 1)
	assert.Contains(t, w[0], "SP_PROXY_S3_SSE")
}

func TestWarnings_LocalBackendNoWarnings(t *testing.T) {
	cfg := Load()
	cfg.Bodies.Enabled = true
	cfg.Bodies.Backend = "local"
	assert.Empty(t, cfg.Warnings())
}

func TestHasInsecureS3Endpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     bool
	}{
		{"", false},
		{"https://s3.amazonaws.com", false},
		{"HTTPS://s3.amazonaws.com", false},
		{"http://minio.local:9000", true},
		{"HTTP://minio.local:9000", true},
		{"Http://Minio.local", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, hasInsecureS3Endpoint(tt.endpoint), "endpoint=%q", tt.endpoint)
	}
}

func TestDefaults_FailClosedTrue(t *testing.T) {
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "")
	cfg := Load()
	assert.True(t, cfg.PII.FailClosed, "FAIL_CLOSED must default to true (DPP §2.6)")
}

func TestDefaults_FailClosedExplicitFalse(t *testing.T) {
	// Operator can still opt out (warning fires; tested separately).
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "false")
	cfg := Load()
	assert.False(t, cfg.PII.FailClosed)
}

func TestDefaults_AuditRetention13Months(t *testing.T) {
	t.Setenv("SP_PROXY_PURGE_AUDIT_RETENTION", "")
	cfg := Load()
	assert.Equal(t, "9504h", cfg.Purge.AuditRetention,
		"audit retention must default to 9504h (~13 months) per DPP §2.6 (>=12 months) with a buffer")
}

func TestDefaults_MetadataRetention30Days(t *testing.T) {
	t.Setenv("SP_PROXY_PURGE_METADATA_RETENTION", "")
	cfg := Load()
	assert.Equal(t, "720h", cfg.Purge.MetadataRetention,
		"metadata retention must default to 720h (30d) so request_logs stay well within the DPP §1.7 18-month ceiling")
}

func TestDefaults_DashboardBindAddrLoopback(t *testing.T) {
	t.Setenv("SP_PROXY_DASHBOARD_BIND_ADDR", "")
	cfg := Load()
	assert.Equal(t, "127.0.0.1", cfg.Server.DashboardBindAddr,
		"dashboard bind addr must default to loopback so accidental Docker port-forwards do not expose it")
}

func TestDashboardBindAddr_FromEnv(t *testing.T) {
	t.Setenv("SP_PROXY_DASHBOARD_BIND_ADDR", "0.0.0.0")
	cfg := Load()
	assert.Equal(t, "0.0.0.0", cfg.Server.DashboardBindAddr)
}

// productionConfigBase returns a Config that is production-mode and DPP-conformant.
// Each warnings test starts from this and breaks one knob.
func productionConfigBase(t *testing.T) *Config {
	t.Helper()
	t.Setenv("SP_PROXY_ENV", "production")
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "true")
	t.Setenv("SP_PROXY_CACHE_EXCLUDE_PII", "true")
	t.Setenv("SP_PROXY_BODIES_ARCHIVE_MAX_AGE", "720h")
	t.Setenv("SP_PROXY_PURGE_METADATA_RETENTION", "720h")
	t.Setenv("SP_PROXY_PURGE_AUDIT_RETENTION", "9504h")
	t.Setenv("SP_PROXY_DASHBOARD_BIND_ADDR", "127.0.0.1")
	t.Setenv("SP_PROXY_REGION_BIND_ADDR", "127.0.0.1")
	t.Setenv("SP_PROXY_BODIES_BACKEND", "local")
	return Load()
}

func TestProductionWarnings_BaseIsClean(t *testing.T) {
	cfg := productionConfigBase(t)
	assert.Empty(t, cfg.Warnings(),
		"a DPP-conformant production config must produce no warnings")
}

func TestProductionWarnings_FailClosedFalse(t *testing.T) {
	productionConfigBase(t)
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "false")
	cfg := Load()
	w := strings.Join(cfg.Warnings(), "\n")
	assert.Contains(t, w, "SP_PROXY_PII_FAIL_CLOSED")
	assert.Contains(t, w, "DPP")
}

func TestProductionWarnings_CacheExcludePIIFalse(t *testing.T) {
	productionConfigBase(t)
	t.Setenv("SP_PROXY_CACHE_EXCLUDE_PII", "false")
	cfg := Load()
	w := strings.Join(cfg.Warnings(), "\n")
	assert.Contains(t, w, "SP_PROXY_CACHE_EXCLUDE_PII")
}

func TestProductionWarnings_LongArchiveRetention(t *testing.T) {
	productionConfigBase(t)
	t.Setenv("SP_PROXY_BODIES_ARCHIVE_MAX_AGE", "1440h") // 60d
	cfg := Load()
	w := strings.Join(cfg.Warnings(), "\n")
	assert.Contains(t, w, "SP_PROXY_BODIES_ARCHIVE_MAX_AGE")
	assert.Contains(t, w, "30d")
}

func TestProductionWarnings_LongMetadataRetention(t *testing.T) {
	productionConfigBase(t)
	t.Setenv("SP_PROXY_PURGE_METADATA_RETENTION", "14400h") // ~20 months
	cfg := Load()
	w := strings.Join(cfg.Warnings(), "\n")
	assert.Contains(t, w, "SP_PROXY_PURGE_METADATA_RETENTION")
	assert.Contains(t, w, "18 months")
}

func TestProductionWarnings_ShortAuditRetention(t *testing.T) {
	productionConfigBase(t)
	t.Setenv("SP_PROXY_PURGE_AUDIT_RETENTION", "720h") // 30d
	cfg := Load()
	w := strings.Join(cfg.Warnings(), "\n")
	assert.Contains(t, w, "SP_PROXY_PURGE_AUDIT_RETENTION")
	assert.Contains(t, w, "12 months")
}

func TestProductionWarnings_NonLoopbackDashboard(t *testing.T) {
	productionConfigBase(t)
	t.Setenv("SP_PROXY_DASHBOARD_BIND_ADDR", "0.0.0.0")
	cfg := Load()
	w := strings.Join(cfg.Warnings(), "\n")
	assert.Contains(t, w, "SP_PROXY_DASHBOARD_BIND_ADDR")
}

func TestProductionWarnings_DevelopmentSilent(t *testing.T) {
	t.Setenv("SP_PROXY_ENV", "development")
	t.Setenv("SP_PROXY_PII_FAIL_CLOSED", "false") // would warn in prod
	cfg := Load()
	for _, w := range cfg.Warnings() {
		assert.NotContains(t, w, "SP_PROXY_PII_FAIL_CLOSED",
			"DPP warnings must be production-only")
	}
}

func TestLoad_RegionBindAddr_Default(t *testing.T) {
	t.Setenv("SP_PROXY_PORT_EU", "8080")
	cfg := Load()
	assert.Equal(t, "127.0.0.1", cfg.Server.RegionBindAddr,
		"RegionBindAddr defaults to 127.0.0.1 (loopback-only sidecar)")
}

func TestLoad_RegionBindAddr_FromEnv(t *testing.T) {
	t.Setenv("SP_PROXY_PORT_EU", "8080")
	t.Setenv("SP_PROXY_REGION_BIND_ADDR", "0.0.0.0")
	cfg := Load()
	assert.Equal(t, "0.0.0.0", cfg.Server.RegionBindAddr)
}

func TestWarnings_NonLoopbackRegionBindInProduction(t *testing.T) {
	t.Setenv("SP_PROXY_PORT_EU", "8080")
	t.Setenv("SP_PROXY_REGION_BIND_ADDR", "0.0.0.0")
	t.Setenv("SP_PROXY_ENV", "production")
	cfg := Load()
	warnings := cfg.Warnings()
	var found bool
	for _, w := range warnings {
		if strings.Contains(w, "SP_PROXY_REGION_BIND_ADDR") && strings.Contains(w, "non-loopback") {
			found = true
			break
		}
	}
	assert.True(t, found, "production + non-loopback region bind must emit a warning, got %v", warnings)
}
