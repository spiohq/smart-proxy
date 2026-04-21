package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	Enabled        bool
	DefaultMode    string            // "queue", "reject", "queue-timeout"
	QueueTimeout   string            // Duration string, e.g. "60s"
	QueueMaxDepth  int
	ThrottleFactor float64           // 0.0-1.0, default 0.8
	BucketTTL      string            // Duration string for GC, e.g. "2h"
	MerchantModes  map[string]string // merchant_key → throttle mode override
	EndpointModes  map[string]string // endpoint pattern → throttle mode override
}

// CacheConfig holds caching settings.
type CacheConfig struct {
	Enabled    bool
	MaxMemory  int64  // bytes, default 256 MB
	DefaultTTL string // duration string, e.g. "60s"
	ExcludePII bool   // when true, PII checker can skip caching
}

// StorageConfig holds database storage settings.
type StorageConfig struct {
	Backend    string // "sqlite" (default)
	SQLitePath string // Path to SQLite database file
}

// BodiesConfig holds request/response body storage settings.
type BodiesConfig struct {
	Enabled        bool
	BasePath       string // Base directory for body files
	RecentMaxAge   string // Duration string, e.g. "24h"
	ArchiveMaxAge  string // Duration string, e.g. "720h" (30 days)
	Compression    string // Codec for archive tier: "zstd" (default), "gzip", "none"
	MaxCaptureSize int64  // Per-message byte cap for request/response bodies
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
	Server     ServerConfig
	RateLimit  RateLimitConfig
	Cache      CacheConfig
	Storage    StorageConfig
	Bodies     BodiesConfig
	Purge      PurgeConfig
	Prometheus PrometheusConfig
	RDT        RDTConfig
}

// RDTConfig holds settings for automatic RDT (Restricted Data Token) handling.
type RDTConfig struct {
	AutoMint bool // When true, proxy automatically mints RDTs for PII endpoints
}

// ServerConfig holds port bindings and shutdown timeout.
// Port convention: >0 = listen on that port, 0 = disabled (skip).
// In tests, use port 0 with net.Listen to let the OS assign a free port;
// but for config semantics, 0 means "don't start this listener".
type ServerConfig struct {
	PortEU          int
	PortNA          int
	PortFE          int
	PortDashboard   int
	ShutdownTimeout string
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
		Server: ServerConfig{
			PortEU:          iInt("SP_PROXY_PORT_EU", 8080),
			PortNA:          iInt("SP_PROXY_PORT_NA", 8081),
			PortFE:          iInt("SP_PROXY_PORT_FE", 8082),
			PortDashboard:   iInt("SP_PROXY_PORT_DASHBOARD", 9090),
			ShutdownTimeout: envStr("SP_PROXY_SHUTDOWN_TIMEOUT", "30s"),
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
			Enabled:    iBool("SP_PROXY_CACHE_ENABLED", true),
			MaxMemory:  iInt64("SP_PROXY_CACHE_MAX_MEMORY", 268435456),
			DefaultTTL: envStr("SP_PROXY_CACHE_DEFAULT_TTL", "60s"),
			ExcludePII: iBool("SP_PROXY_CACHE_EXCLUDE_PII", true),
		},
		Storage: StorageConfig{
			Backend:    envStr("SP_PROXY_STORAGE_BACKEND", "sqlite"),
			SQLitePath: envStr("SP_PROXY_SQLITE_PATH", "/data/sp-proxy.db"),
		},
		Bodies: BodiesConfig{
			Enabled:        iBool("SP_PROXY_BODIES_ENABLED", true),
			BasePath:       envStr("SP_PROXY_BODIES_PATH", "/data/bodies"),
			RecentMaxAge:   envStr("SP_PROXY_BODIES_RECENT_MAX_AGE", "24h"),
			ArchiveMaxAge:  envStr("SP_PROXY_BODIES_ARCHIVE_MAX_AGE", "720h"),
			Compression:    envStr("SP_PROXY_BODIES_COMPRESSION", "zstd"),
			MaxCaptureSize: iInt64("SP_PROXY_BODIES_MAX_CAPTURE_SIZE", 256*1024),
		},
		Purge: PurgeConfig{
			MetadataRetention: envStr("SP_PROXY_PURGE_METADATA_RETENTION", "720h"),
			AuditRetention:    envStr("SP_PROXY_PURGE_AUDIT_RETENTION", "8760h"),
		},
		Prometheus: PrometheusConfig{
			Enabled: iBool("SP_PROXY_PROMETHEUS_ENABLED", true),
			Port:    iInt("SP_PROXY_PROMETHEUS_PORT", 0),
			Path:    envStr("SP_PROXY_PROMETHEUS_PATH", "/metrics"),
		},
		RDT: RDTConfig{
			AutoMint: iBool("SP_PROXY_RDT_AUTO_MINT", false),
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
	s := c.Server
	if s.PortEU <= 0 && s.PortNA <= 0 && s.PortFE <= 0 {
		return fmt.Errorf("at least one proxy port must be enabled (non-zero)")
	}
	if s.PortDashboard <= 0 {
		return fmt.Errorf("dashboard port must be set (SP_PROXY_PORT_DASHBOARD)")
	}
	if c.RateLimit.ThrottleFactor <= 0 || c.RateLimit.ThrottleFactor > 1.0 {
		return fmt.Errorf("throttle factor must be in (0, 1.0], got %f", c.RateLimit.ThrottleFactor)
	}
	if c.RateLimit.QueueMaxDepth <= 0 {
		return fmt.Errorf("queue max depth must be positive, got %d", c.RateLimit.QueueMaxDepth)
	}
	if err := validatePositiveDuration("SP_PROXY_BUCKET_TTL", c.RateLimit.BucketTTL); err != nil {
		return err
	}
	if c.Cache.Enabled {
		if c.Cache.MaxMemory <= 0 {
			return fmt.Errorf("cache max memory must be positive, got %d", c.Cache.MaxMemory)
		}
		if err := validatePositiveDuration("SP_PROXY_CACHE_DEFAULT_TTL", c.Cache.DefaultTTL); err != nil {
			return fmt.Errorf("invalid cache default TTL %q: %w", c.Cache.DefaultTTL, err)
		}
	}
	if c.Storage.Backend != "sqlite" {
		return fmt.Errorf("unsupported storage backend: %s (only 'sqlite' supported)", c.Storage.Backend)
	}
	if c.Bodies.Enabled {
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
	}
	for _, d := range []struct{ name, val string }{
		{"SP_PROXY_PURGE_METADATA_RETENTION", c.Purge.MetadataRetention},
		{"SP_PROXY_PURGE_AUDIT_RETENTION", c.Purge.AuditRetention},
	} {
		if err := validatePositiveDuration(d.name, d.val); err != nil {
			return err
		}
	}
	// Validate parent directories exist
	if dir := filepath.Dir(c.Storage.SQLitePath); c.Storage.SQLitePath != ":memory:" {
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
