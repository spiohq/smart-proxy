//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/dashboard"
	"github.com/spiohq/smart-proxy/internal/logging"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/proxy"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
	"github.com/spiohq/smart-proxy/internal/rdt"
	"github.com/spiohq/smart-proxy/internal/server"
	"github.com/spiohq/smart-proxy/internal/storage"
	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/require"
)

type TestEnv struct {
	Server    *server.Server
	MockSPAPI *mock.MockSPAPI
	Config    *config.Config
	ProxyURL  string
	DashURL   string
}

type testEnvExtras struct {
	tokenMap     map[string]string
	piiFailClose bool
}

type Option func(*config.Config, *testEnvExtras)

func WithCacheDisabled() Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.Cache.Enabled = false }
}

func WithRateLimitMode(mode string) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.DefaultMode = mode }
}

func WithRateLimitDisabled() Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.Enabled = false }
}

func WithQueueTimeout(timeout string) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.QueueTimeout = timeout }
}

func WithQueueMaxDepth(depth int) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.QueueMaxDepth = depth }
}

func WithThrottleFactor(factor float64) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.ThrottleFactor = factor }
}

func WithMerchantModes(modes map[string]string) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.MerchantModes = modes }
}

func WithEndpointModes(modes map[string]string) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RateLimit.EndpointModes = modes }
}

func WithCachePIIExclusion(exclude bool) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.Cache.ExcludePII = exclude }
}

func WithCacheTTL(ttl string) Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.Cache.DefaultTTL = ttl }
}

func WithTokenMap(tokenMap map[string]string) Option {
	return func(_ *config.Config, extras *testEnvExtras) { extras.tokenMap = tokenMap }
}

func WithRDTAutoMint() Option {
	return func(cfg *config.Config, _ *testEnvExtras) { cfg.RDT.AutoMint = true }
}

func WithPIIFailClosed() Option {
	return func(cfg *config.Config, extras *testEnvExtras) {
		cfg.PII.FailClosed = true
		extras.piiFailClose = true
	}
}

func NewTestEnv(t *testing.T, opts ...Option) *TestEnv {
	t.Helper()

	mockAPI := mock.NewMockSPAPI()

	metaStore, err := storage.NewSQLiteStore(":memory:")
	require.NoError(t, err)

	bodyDir := t.TempDir()
	bodyStore, err := bodies.NewLocalStore(bodyDir)
	require.NoError(t, err)

	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:          freePort(t),
			PortNA:          0,
			PortFE:          0,
			PortDashboard:   freePort(t),
			ShutdownTimeout: "5s",
		},
		RateLimit: config.RateLimitConfig{
			Enabled:        true,
			DefaultMode:    "queue",
			QueueTimeout:   "5s",
			QueueMaxDepth:  10,
			ThrottleFactor: 0.8,
			BucketTTL:      "2h",
		},
		Cache: config.CacheConfig{
			Enabled:    true,
			MaxMemory:  67108864,
			DefaultTTL: "60s",
			ExcludePII: true,
		},
		Storage: config.StorageConfig{
			Backend:    "sqlite",
			SQLitePath: ":memory:",
		},
		Bodies: config.BodiesConfig{
			Enabled:       true,
			BasePath:      bodyDir,
			RecentMaxAge:  "72h",
			ArchiveMaxAge: "8760h",
		},
		Purge: config.PurgeConfig{
			MetadataRetention: "720h",
			AuditRetention:    "8760h",
		},
	}

	extras := &testEnvExtras{}
	for _, opt := range opts {
		opt(cfg, extras)
	}

	limiter := ratelimit.NewLimiter(cfg.RateLimit.ThrottleFactor, cfg.RateLimit.QueueMaxDepth)
	rlMiddleware := ratelimit.RateLimitMiddleware(limiter, &cfg.RateLimit)

	registry := pii.NewRegistryWithExtras(cfg.PII.QueryParamsExtra)
	registry.SetFailClosed(extras.piiFailClose || cfg.PII.FailClosed)
	var cacheMiddleware proxy.Middleware
	if cfg.Cache.Enabled {
		mc := cache.NewMemoryCache(cfg.Cache.MaxMemory)
		tc := cache.NewTierClassifier(registry.ContainsPII)
		cacheMiddleware = cache.CacheMiddleware(mc, tc, &cfg.Cache)
		t.Cleanup(func() { mc.Close() })
	} else {
		cacheMiddleware = func(next http.Handler) http.Handler { return next }
	}

	piiEngine := pii.NewEngine(registry)
	asyncLogger := logging.NewAsyncLogger(metaStore, bodyStore, piiEngine, 1000)
	resolver := merchant.NewResolver(extras.tokenMap)

	auditStore := audit.NewSQLiteStore(metaStore.DB())

	dashHandler := dashboard.NewHandler(metaStore, auditStore, bodyStore)
	dashMux := dashboard.NewMux(dashHandler)

	mockHost := mockAPI.Listener.Addr().String()

	// RDT middleware (when enabled, mints via the same mock SP-API)
	var rdtMiddleware proxy.Middleware
	if cfg.RDT.AutoMint {
		rdtCache := rdt.NewCache(5 * time.Minute)
		reportTracker := rdt.NewReportTracker(70 * time.Minute)
		minter := rdt.NewMinter("http://"+mockHost, &http.Client{Timeout: 5 * time.Second})
		rdtMW := rdt.NewMiddleware(rdtCache, minter, reportTracker)
		rdtMiddleware = rdtMW.Handler
	} else {
		rdtMiddleware = func(next http.Handler) http.Handler { return next }
	}

	factory := func(region server.Region) http.Handler {
		rp := proxy.NewTestProxyWithLimiter(mockHost, limiter)
		logMiddleware := logging.LoggingMiddleware(asyncLogger, registry, string(region), 0)
		return proxy.BuildChain(rp, resolver.Middleware(), logMiddleware, rdtMiddleware, cacheMiddleware, rlMiddleware)
	}

	srv, err := server.New(cfg, factory, dashMux)
	require.NoError(t, err)

	go srv.Start()

	require.Eventually(t, func() bool {
		return srv.DashboardAddr() != "" && srv.RegionAddr(server.RegionEU) != ""
	}, 3*time.Second, 50*time.Millisecond)

	env := &TestEnv{
		Server:    srv,
		MockSPAPI: mockAPI,
		Config:    cfg,
		ProxyURL:  fmt.Sprintf("http://%s", srv.RegionAddr(server.RegionEU)),
		DashURL:   fmt.Sprintf("http://%s", srv.DashboardAddr()),
	}

	t.Cleanup(func() {
		srv.Shutdown()
		asyncLogger.Close()
		metaStore.Close()
		bodyStore.Close()
		mockAPI.Close()
		limiter.Stop()
	})

	return env
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
