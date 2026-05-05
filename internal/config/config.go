package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	Enabled        bool
	DefaultMode    string // "queue", "reject", "queue-timeout"
	QueueTimeout   string // Duration string, e.g. "60s"
	QueueMaxDepth  int
	ThrottleFactor float64           // 0.0-1.0, default 0.8
	BucketTTL      string            // Duration string for GC, e.g. "2h"
	MerchantModes  map[string]string // merchant_key → throttle mode override
	EndpointModes  map[string]string // endpoint pattern → throttle mode override
}

// CacheConfig holds caching settings.
type CacheConfig struct {
	Enabled      bool
	MaxMemory    int64  // bytes, default 256 MB
	DefaultTTL   string // duration string, e.g. "60s"
	MaxClientTTL string // duration string; clamps client-supplied X-SP-Proxy-Cache-TTL / Cache-Until
	ExcludePII   bool   // when true, PII checker can skip caching
}

// PIIConfig holds PII engine settings.
type PIIConfig struct {
	// FailClosed makes the PII engine treat any path it does not recognize as
	// PII-bearing. New SP-API endpoints added upstream are then redacted and
	// excluded from cache by default until the registry is updated.
	FailClosed       bool
	QueryParamsExtra []string // SP_PROXY_PII_QUERY_PARAMS=foo,bar
}

// StorageConfig holds database storage settings.
type StorageConfig struct {
	Backend    string // "sqlite" (default)
	SQLitePath string // Path to SQLite database file
}

// BodiesConfig holds request/response body storage settings.
type BodiesConfig struct {
	Enabled        bool
	BasePath       string // Base directory for body files (also holds current/ when Backend=s3)
	Backend        string // "local" (default) or "s3"
	RecentMaxAge   string // Duration string, e.g. "24h"
	ArchiveMaxAge  string // Duration string, e.g. "720h" (30 days)
	Compression    string // Codec for archive tier: "zstd" (default), "gzip", "none"
	MaxCaptureSize int64  // Per-message byte cap for request/response bodies
	MaxBytes       int64  // Hard cap across current/+recent/+archive/; 0 disables eviction
	S3             S3Config
}

// S3Config holds S3 (or S3-compatible) backend settings. Only used when
// BodiesConfig.Backend = "s3".
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string // Custom endpoint for MinIO/R2; empty for real AWS
	AccessKey string
	SecretKey string
	PathStyle bool   // Required for MinIO; harmless for AWS
	SSE       string // Server-side encryption: "" | "AES256" | "aws:kms" | "aws:kms:dsse"
	SSEKMSKey string // KMS key ARN/alias for SSE=aws:kms{,:dsse}; optional
}

// PurgeConfig holds purge/retention settings for background jobs.
type PurgeConfig struct {
	MetadataRetention string
	AuditRetention    string
}

// PrometheusConfig holds Prometheus metrics settings.
type PrometheusConfig struct {
	Enabled bool
	Port    int    // Port for /metrics endpoint; 0 = serve on dashboard port
	Path    string // HTTP path, default "/metrics"
}

// Config holds all configuration for the proxy. Only ServerConfig is populated
// in Phase 1  -  other sub-configs will be added in later phases.
type Config struct {
	// Env is "production" | "development" (default). In production mode the
	// proxy refuses to start with insecure defaults (e.g. plain-http S3 endpoints).
	Env        string
	Server     ServerConfig
	RateLimit  RateLimitConfig
	Cache      CacheConfig
	PII        PIIConfig
	Storage    StorageConfig
	Bodies     BodiesConfig
	Purge      PurgeConfig
	Prometheus PrometheusConfig
	RDT        RDTConfig
	Validation ValidationConfig
}

// RDTConfig holds settings for automatic RDT (Restricted Data Token) handling.
type RDTConfig struct {
	AutoMint bool // When true, proxy automatically mints RDTs for PII endpoints
}

// ValidationConfig holds settings for proxy-side OpenAPI request validation.
type ValidationConfig struct {
	Enabled         bool
	SpecsURL        string
	SpecsDir        string
	RefreshInterval string
}

// ServerConfig holds port bindings and shutdown timeout.
// Port convention: >0 = listen on that port, 0 = disabled (skip).
// In tests, use port 0 with net.Listen to let the OS assign a free port;
// but for config semantics, 0 means "don't start this listener".
type ServerConfig struct {
	PortEU            int
	PortNA            int
	PortFE            int
	PortDashboard     int
	DashboardBindAddr string // SP_PROXY_DASHBOARD_BIND_ADDR; "127.0.0.1" by default
	RegionBindAddr    string // SP_PROXY_REGION_BIND_ADDR; "127.0.0.1" by default
	StrictMerchant    bool   // SP_PROXY_STRICT_MERCHANT; reject requests with no resolvable merchant
	ShutdownTimeout   string
}

// Load reads configuration from environment variables with defaults.
// Set a port to 0 to disable that region (e.g. SP_PROXY_PORT_NA=0).
func Load() *Config {
	return loadConfig(nil)
}

// LoadWithLogger reads configuration from environment variables with defaults,
// logging warnings for any invalid values that fall back to defaults.
func LoadWithLogger(logger *slog.Logger) *Config {
	return loadConfig(logger)
}

func loadConfig(logger *slog.Logger) *Config {
	iInt := envInt
	iInt64 := envInt64
	iFloat := envFloat
	iBool := envBool
	if logger != nil {
		iInt = func(key string, fallback int) int { return envIntLog(logger, key, fallback) }
		iInt64 = func(key string, fallback int64) int64 { return envInt64Log(logger, key, fallback) }
		iFloat = func(key string, fallback float64) float64 { return envFloatLog(logger, key, fallback) }
		iBool = func(key string, fallback bool) bool { return envBoolLog(logger, key, fallback) }
	}
	return &Config{
		Env: envStr("SP_PROXY_ENV", "development"),
		Server: ServerConfig{
			PortEU:            iInt("SP_PROXY_PORT_EU", 8080),
			PortNA:            iInt("SP_PROXY_PORT_NA", 8081),
			PortFE:            iInt("SP_PROXY_PORT_FE", 8082),
			PortDashboard:     iInt("SP_PROXY_PORT_DASHBOARD", 9090),
			DashboardBindAddr: envStr("SP_PROXY_DASHBOARD_BIND_ADDR", "127.0.0.1"),
			RegionBindAddr:    envStr("SP_PROXY_REGION_BIND_ADDR", "127.0.0.1"),
			StrictMerchant:    iBool("SP_PROXY_STRICT_MERCHANT", false),
			ShutdownTimeout:   envStr("SP_PROXY_SHUTDOWN_TIMEOUT", "30s"),
		},
		RateLimit: RateLimitConfig{
			Enabled:        iBool("SP_PROXY_RATELIMIT_ENABLED", true),
			DefaultMode:    envStr("SP_PROXY_RATELIMIT_MODE", "queue"),
			QueueTimeout:   envStr("SP_PROXY_RATELIMIT_QUEUE_TIMEOUT", "60s"),
			QueueMaxDepth:  iInt("SP_PROXY_RATELIMIT_QUEUE_MAX_DEPTH", 100),
			ThrottleFactor: iFloat("SP_PROXY_RATELIMIT_THROTTLE_FACTOR", 0.8),
			BucketTTL:      envStr("SP_PROXY_BUCKET_TTL", "2h"),
		},
		Cache: CacheConfig{
			Enabled:      iBool("SP_PROXY_CACHE_ENABLED", true),
			MaxMemory:    iInt64("SP_PROXY_CACHE_MAX_MEMORY", 268435456),
			DefaultTTL:   envStr("SP_PROXY_CACHE_DEFAULT_TTL", "60s"),
			MaxClientTTL: envStr("SP_PROXY_CACHE_MAX_CLIENT_TTL", "24h"),
			ExcludePII:   iBool("SP_PROXY_CACHE_EXCLUDE_PII", true),
		},
		PII: PIIConfig{
			FailClosed:       iBool("SP_PROXY_PII_FAIL_CLOSED", true),
			QueryParamsExtra: envStrSlice("SP_PROXY_PII_QUERY_PARAMS"),
		},
		Storage: StorageConfig{
			Backend:    envStr("SP_PROXY_STORAGE_BACKEND", "sqlite"),
			SQLitePath: envStr("SP_PROXY_SQLITE_PATH", "/data/sp-proxy.db"),
		},
		Bodies: BodiesConfig{
			Enabled:        iBool("SP_PROXY_BODIES_ENABLED", true),
			BasePath:       envStr("SP_PROXY_BODIES_PATH", "/data/bodies"),
			Backend:        envStr("SP_PROXY_BODIES_BACKEND", "local"),
			RecentMaxAge:   envStr("SP_PROXY_BODIES_RECENT_MAX_AGE", "24h"),
			ArchiveMaxAge:  envStr("SP_PROXY_BODIES_ARCHIVE_MAX_AGE", "720h"),
			Compression:    envStr("SP_PROXY_BODIES_COMPRESSION", "zstd"),
			MaxCaptureSize: iInt64("SP_PROXY_BODIES_MAX_CAPTURE_SIZE", 256*1024),
			MaxBytes:       iInt64("SP_PROXY_BODIES_MAX_BYTES", 8*1024*1024*1024),
			S3: S3Config{
				Bucket:    envStr("SP_PROXY_S3_BUCKET", ""),
				Region:    envStr("SP_PROXY_S3_REGION", ""),
				Endpoint:  envStr("SP_PROXY_S3_ENDPOINT", ""),
				AccessKey: envStr("SP_PROXY_S3_ACCESS_KEY", ""),
				SecretKey: envStr("SP_PROXY_S3_SECRET_KEY", ""),
				PathStyle: iBool("SP_PROXY_S3_PATH_STYLE", false),
				SSE:       envStr("SP_PROXY_S3_SSE", ""),
				SSEKMSKey: envStr("SP_PROXY_S3_SSE_KMS_KEY", ""),
			},
		},
		Purge: PurgeConfig{
			MetadataRetention: envStr("SP_PROXY_PURGE_METADATA_RETENTION", "720h"),
			AuditRetention:    envStr("SP_PROXY_PURGE_AUDIT_RETENTION", "9504h"),
		},
		Prometheus: PrometheusConfig{
			Enabled: iBool("SP_PROXY_PROMETHEUS_ENABLED", true),
			Port:    iInt("SP_PROXY_PROMETHEUS_PORT", 0),
			Path:    envStr("SP_PROXY_PROMETHEUS_PATH", "/metrics"),
		},
		RDT: RDTConfig{
			AutoMint: iBool("SP_PROXY_RDT_AUTO_MINT", false),
		},
		Validation: ValidationConfig{
			Enabled:         iBool("SP_PROXY_VALIDATION_ENABLED", false),
			SpecsURL:        envStr("SP_PROXY_VALIDATION_SPECS_URL", "https://github.com/amzn/selling-partner-api-models/archive/refs/heads/main.zip"),
			SpecsDir:        envStr("SP_PROXY_VALIDATION_SPECS_DIR", ""),
			RefreshInterval: envStr("SP_PROXY_VALIDATION_REFRESH_INTERVAL", "24h"),
		},
	}
}

// RegionPort returns the configured port for a given region.
// Returns 0 (disabled) if the region is unknown.
func (s *ServerConfig) RegionPort(region string) int {
	switch region {
	case "eu":
		return s.PortEU
	case "na":
		return s.PortNA
	case "fe":
		return s.PortFE
	default:
		return 0
	}
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envIntLog(logger *slog.Logger, key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
		logger.Warn("invalid env var, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func envInt64Log(logger *slog.Logger, key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
		logger.Warn("invalid env var, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func envFloatLog(logger *slog.Logger, key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
		logger.Warn("invalid env var, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

func envBoolLog(logger *slog.Logger, key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		logger.Warn("invalid env var, using default", "key", key, "value", v, "default", fallback)
	}
	return fallback
}

// envStrSlice parses a comma-separated env var into a slice. Empty strings
// after splitting are dropped. Whitespace around items is trimmed.
func envStrSlice(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validatePositiveDuration(name, val string) error {
	d, err := time.ParseDuration(val)
	if err != nil {
		return fmt.Errorf("invalid duration for %s: %q: %w", name, val, err)
	}
	if d <= 0 {
		return fmt.Errorf("duration for %s must be positive, got %s", name, val)
	}
	return nil
}

// Validate checks that the configuration is valid.
// A port of 0 means "disabled"  -  at least one region must be enabled.
func (c *Config) Validate() error {
	if err := c.validateServer(); err != nil {
		return err
	}
	if err := c.validateRateLimit(); err != nil {
		return err
	}
	if err := c.validateCache(); err != nil {
		return err
	}
	if c.Storage.Backend != "sqlite" {
		return fmt.Errorf("unsupported storage backend: %s (only 'sqlite' supported)", c.Storage.Backend)
	}
	if err := c.validateBodies(); err != nil {
		return err
	}
	if err := c.validatePurge(); err != nil {
		return err
	}
	return c.validatePaths()
}

func (c *Config) validateServer() error {
	s := c.Server
	if s.PortEU <= 0 && s.PortNA <= 0 && s.PortFE <= 0 {
		return fmt.Errorf("at least one proxy port must be enabled (non-zero)")
	}
	if s.PortDashboard <= 0 {
		return fmt.Errorf("dashboard port must be set (SP_PROXY_PORT_DASHBOARD)")
	}
	return nil
}

func (c *Config) validateRateLimit() error {
	if c.RateLimit.ThrottleFactor <= 0 || c.RateLimit.ThrottleFactor > 1.0 {
		return fmt.Errorf("throttle factor must be in (0, 1.0], got %f", c.RateLimit.ThrottleFactor)
	}
	if c.RateLimit.QueueMaxDepth <= 0 {
		return fmt.Errorf("queue max depth must be positive, got %d", c.RateLimit.QueueMaxDepth)
	}
	return validatePositiveDuration("SP_PROXY_BUCKET_TTL", c.RateLimit.BucketTTL)
}

func (c *Config) validateCache() error {
	if !c.Cache.Enabled {
		return nil
	}
	if c.Cache.MaxMemory <= 0 {
		return fmt.Errorf("cache max memory must be positive, got %d", c.Cache.MaxMemory)
	}
	if err := validatePositiveDuration("SP_PROXY_CACHE_DEFAULT_TTL", c.Cache.DefaultTTL); err != nil {
		return fmt.Errorf("invalid cache default TTL %q: %w", c.Cache.DefaultTTL, err)
	}
	return nil
}

func (c *Config) validateBodies() error {
	if !c.Bodies.Enabled {
		return nil
	}
	if err := validatePositiveDuration("SP_PROXY_BODIES_RECENT_MAX_AGE", c.Bodies.RecentMaxAge); err != nil {
		return fmt.Errorf("invalid bodies recent max age %q: %w", c.Bodies.RecentMaxAge, err)
	}
	if err := validatePositiveDuration("SP_PROXY_BODIES_ARCHIVE_MAX_AGE", c.Bodies.ArchiveMaxAge); err != nil {
		return fmt.Errorf("invalid bodies archive max age %q: %w", c.Bodies.ArchiveMaxAge, err)
	}
	switch c.Bodies.Compression {
	case "", "zstd", "gzip", "none":
	default:
		return fmt.Errorf("invalid SP_PROXY_BODIES_COMPRESSION %q (want zstd|gzip|none)", c.Bodies.Compression)
	}
	if c.Bodies.MaxCaptureSize <= 0 {
		return fmt.Errorf("SP_PROXY_BODIES_MAX_CAPTURE_SIZE must be positive, got %d", c.Bodies.MaxCaptureSize)
	}
	if c.Bodies.MaxBytes < 0 {
		return fmt.Errorf("SP_PROXY_BODIES_MAX_BYTES cannot be negative, got %d", c.Bodies.MaxBytes)
	}
	return c.validateBodiesBackend()
}

func (c *Config) validateBodiesBackend() error {
	switch c.Bodies.Backend {
	case "", "local":
		return nil
	case "s3":
		if c.Bodies.S3.Bucket == "" {
			return fmt.Errorf("SP_PROXY_S3_BUCKET is required when SP_PROXY_BODIES_BACKEND=s3")
		}
		switch c.Bodies.S3.SSE {
		case "", "AES256", "aws:kms", "aws:kms:dsse":
		default:
			return fmt.Errorf("invalid SP_PROXY_S3_SSE %q (want AES256|aws:kms|aws:kms:dsse)", c.Bodies.S3.SSE)
		}
		if c.IsProduction() && hasInsecureS3Endpoint(c.Bodies.S3.Endpoint) {
			return fmt.Errorf("SP_PROXY_S3_ENDPOINT %q uses plain http; refuse to start in production (set SP_PROXY_ENV=development to override, or use https)", c.Bodies.S3.Endpoint)
		}
		return nil
	default:
		return fmt.Errorf("invalid SP_PROXY_BODIES_BACKEND %q (want local|s3)", c.Bodies.Backend)
	}
}

func (c *Config) validatePurge() error {
	for _, d := range []struct{ name, val string }{
		{"SP_PROXY_PURGE_METADATA_RETENTION", c.Purge.MetadataRetention},
		{"SP_PROXY_PURGE_AUDIT_RETENTION", c.Purge.AuditRetention},
	} {
		if err := validatePositiveDuration(d.name, d.val); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validatePaths() error {
	if c.Storage.SQLitePath != ":memory:" {
		dir := filepath.Dir(c.Storage.SQLitePath)
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("SQLite path parent directory %q does not exist: %w", dir, err)
		}
	}
	if c.Bodies.Enabled {
		if dir := filepath.Dir(c.Bodies.BasePath); dir != "." {
			if _, err := os.Stat(dir); err != nil {
				return fmt.Errorf("bodies base path parent directory %q does not exist: %w", dir, err)
			}
		}
	}
	return nil
}

// IsProduction reports whether SP_PROXY_ENV is set to production.
// The check is case-insensitive.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.Env, "production")
}

// Warnings returns non-fatal configuration concerns surfaced for logging.
// In production these are upgraded to errors by Validate(); in development
// they are advisory.
func (c *Config) Warnings() []string {
	w := c.s3Warnings()
	if !c.IsProduction() {
		return w
	}
	w = append(w, c.productionDPPWarnings()...)
	w = append(w, c.productionBindWarnings()...)
	return w
}

// s3Warnings reports S3-related concerns that apply in any environment.
func (c *Config) s3Warnings() []string {
	if !c.Bodies.Enabled || c.Bodies.Backend != "s3" {
		return nil
	}
	var w []string
	if hasInsecureS3Endpoint(c.Bodies.S3.Endpoint) {
		w = append(w, fmt.Sprintf("SP_PROXY_S3_ENDPOINT %q uses plain http; SigV4 credentials and PII-redacted bodies travel unencrypted. Use https in production.", c.Bodies.S3.Endpoint))
	}
	if c.Bodies.S3.SSE == "" {
		w = append(w, "SP_PROXY_S3_SSE is empty; relying on bucket-default encryption. Set SP_PROXY_S3_SSE=AES256 (or aws:kms) to enforce server-side encryption explicitly.")
	}
	return w
}

// productionDPPWarnings reports DPP/AUP concerns that only matter when
// SP_PROXY_ENV=production. Each warning maps to a specific DPP clause.
func (c *Config) productionDPPWarnings() []string {
	var w []string
	if !c.PII.FailClosed {
		w = append(w, "SP_PROXY_PII_FAIL_CLOSED=false in production: unknown SP-API endpoints will be logged unredacted, violating DPP §2.6.")
	}
	if !c.Cache.ExcludePII {
		w = append(w, "SP_PROXY_CACHE_EXCLUDE_PII=false in production: PII responses will be cached, violating DPP §2.1 retention.")
	}
	if d, err := time.ParseDuration(c.Bodies.ArchiveMaxAge); err == nil && d > 30*24*time.Hour {
		w = append(w, fmt.Sprintf("SP_PROXY_BODIES_ARCHIVE_MAX_AGE=%s exceeds 30d in production: PII bodies retained beyond DPP §2.1 limit.", c.Bodies.ArchiveMaxAge))
	}
	if d, err := time.ParseDuration(c.Purge.MetadataRetention); err == nil && d > 18*30*24*time.Hour {
		w = append(w, fmt.Sprintf("SP_PROXY_PURGE_METADATA_RETENTION=%s exceeds 18 months in production: violates DPP §1.7 non-PII retention limit.", c.Purge.MetadataRetention))
	}
	if d, err := time.ParseDuration(c.Purge.AuditRetention); err == nil && d < 12*30*24*time.Hour {
		w = append(w, fmt.Sprintf("SP_PROXY_PURGE_AUDIT_RETENTION=%s is below 12 months in production: violates DPP §2.6 audit log retention.", c.Purge.AuditRetention))
	}
	return w
}

// productionBindWarnings flags non-loopback bind addresses in production.
// Both the dashboard and the region listener trust upstream HTTP headers, so
// reaching them from the open internet is treated as a misconfiguration.
func (c *Config) productionBindWarnings() []string {
	var w []string
	if !isLoopbackBind(c.Server.DashboardBindAddr) {
		w = append(w, fmt.Sprintf("SP_PROXY_DASHBOARD_BIND_ADDR=%q is non-loopback in production: ensure an authenticating reverse proxy is in front of the dashboard.", c.Server.DashboardBindAddr))
	}
	if !isLoopbackBind(c.Server.RegionBindAddr) {
		w = append(w, fmt.Sprintf("SP_PROXY_REGION_BIND_ADDR=%q is non-loopback in production: callers can reach the proxy without network-level access control. The proxy reads X-SP-Proxy-Merchant-Id without authentication and any reachable client can self-claim any merchant identity.", c.Server.RegionBindAddr))
	}
	return w
}

func isLoopbackBind(addr string) bool {
	switch addr {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// hasInsecureS3Endpoint reports whether the configured endpoint is a plain-http URL.
// An empty endpoint (real AWS) is always considered secure since the SDK uses https.
func hasInsecureS3Endpoint(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	lower := strings.ToLower(endpoint)
	return strings.HasPrefix(lower, "http://")
}
