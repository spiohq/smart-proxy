package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/blob"
	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/dashboard"
	"github.com/spiohq/smart-proxy/internal/logging"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/prommetrics"
	"github.com/spiohq/smart-proxy/internal/proxy"
	"github.com/spiohq/smart-proxy/internal/purge"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
	"github.com/spiohq/smart-proxy/internal/rdt"
	"github.com/spiohq/smart-proxy/internal/scheduler"
	"github.com/spiohq/smart-proxy/internal/server"
	"github.com/spiohq/smart-proxy/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting smart-proxy-for-sp-api",
		"version", "dev",
		"license", "AGPL-3.0",
		"community", "spiohq.com",
	)

	cfg := config.LoadWithLogger(slog.Default())
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	resolver := merchant.NewResolver(nil)
	resolver.SetStrict(cfg.Server.StrictMerchant)
	if cfg.Server.StrictMerchant {
		slog.Info("strict merchant resolution enabled: requests without X-SP-Proxy-Merchant-Id or X-Amz-Access-Token are rejected with 400")
	}

	// Rate limiter
	limiter := ratelimit.NewLimiter(
		cfg.RateLimit.ThrottleFactor,
		cfg.RateLimit.QueueMaxDepth,
	)
	if bucketTTL, err := time.ParseDuration(cfg.RateLimit.BucketTTL); err == nil {
		limiter.StartGC(bucketTTL)
	}
	defer limiter.Stop()

	rlMiddleware := ratelimit.RateLimitMiddleware(limiter, &cfg.RateLimit)

	// Cache + PII
	registry := pii.NewRegistryWithExtras(cfg.PII.QueryParamsExtra)
	registry.SetFailClosed(cfg.PII.FailClosed)
	if cfg.PII.FailClosed {
		slog.Info("PII fail-closed mode enabled: unknown endpoints treated as PII")
	}
	cacheMiddleware, memCache, closeMemCache := setupCacheMiddleware(cfg, registry)
	defer closeMemCache()

	// Storage + Logging (Phase 5)
	metaStore, err := storage.NewSQLiteStore(cfg.Storage.SQLitePath)
	if err != nil {
		slog.Error("failed to create metadata store", "error", err)
		os.Exit(1)
	}
	defer metaStore.Close()

	currentDir := filepath.Join(cfg.Bodies.BasePath, "current")
	bodyBackend, err := newBodyBackend(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to create body backend", "error", err)
		os.Exit(1)
	}
	bodyStore, err := bodies.NewStore(bodyBackend, currentDir)
	if err != nil {
		slog.Error("failed to create body store", "error", err)
		os.Exit(1)
	}
	defer bodyStore.Close()

	piiEngine := pii.NewEngine(registry)
	asyncLogger := logging.NewAsyncLogger(metaStore, bodyStore, piiEngine, 10000)
	defer asyncLogger.Close()

	// Background context for rotator + scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.Bodies.Enabled {
		startBodyRotator(ctx, cfg, bodyBackend, currentDir, metaStore)
	}

	slog.Info("storage enabled",
		"backend", cfg.Storage.Backend,
		"bodiesEnabled", cfg.Bodies.Enabled,
	)

	// Audit logger
	auditStore := audit.NewSQLiteStore(metaStore.DB())
	auditLogger := audit.NewAuditLogger(auditStore)
	_ = auditLogger.Log(ctx, "startup", "main", "proxy starting", map[string]any{
		"version": "dev",
	})

	// Surface configuration warnings as logs AND audit events. The audit-event
	// trail is the operator's evidence, in a DPP audit, that they were warned
	// about non-conformant defaults at startup.
	for _, w := range cfg.Warnings() {
		slog.Warn("configuration warning", "message", w)
		_ = auditLogger.Log(ctx, audit.EventDPPComplianceWarning, "config", w, nil)
	}

	warnIfStaticIAMKey(ctx, cfg, auditLogger)

	// Parse purge retention durations
	metadataRetention, _ := time.ParseDuration(cfg.Purge.MetadataRetention)
	auditRetention, _ := time.ParseDuration(cfg.Purge.AuditRetention)

	// Scheduler
	sched := scheduler.New([]scheduler.Job{
		{Name: "metadata-purge", Fn: purge.MetadataPurgeJob(metaStore, auditLogger, metadataRetention), Interval: 1 * time.Hour},
		{Name: "audit-purge", Fn: purge.AuditPurgeJob(auditStore, auditRetention), Interval: 24 * time.Hour},
	})
	go sched.Run(ctx)

	slog.Info("scheduler started",
		"jobs", 2,
		"metadataRetention", cfg.Purge.MetadataRetention,
	)

	defer func() {
		_ = auditLogger.Log(context.Background(), "shutdown", "main", "proxy stopping", nil)
	}()

	promMetrics, stopPromCollectors := setupPrometheus(cfg, memCache, limiter)
	defer stopPromCollectors()

	// Dashboard handler
	dashHandler := dashboard.NewHandlerWithPII(metaStore, auditStore, bodyStore, piiEngine)
	dashMux := dashboard.NewMux(dashHandler)

	mountMetricsHandler(cfg, dashMux)

	rdtMiddlewareFn := setupRDTMiddleware(cfg)

	factory := buildRegionFactory(cfg, regionFactoryDeps{
		limiter:         limiter,
		asyncLogger:     asyncLogger,
		registry:        registry,
		resolver:        resolver,
		cacheMiddleware: cacheMiddleware,
		rlMiddleware:    rlMiddleware,
		rdtMiddlewareFn: rdtMiddlewareFn,
		promMetrics:     promMetrics,
	})

	// Wrap the dashboard mux with the security-headers middleware (F-03)
	// before handing it to the server. /metrics was already attached above
	// and inherits the headers harmlessly.
	dashRoot := dashboard.SecurityHeadersMiddleware(dashMux)
	srv, err := server.New(cfg, factory, dashRoot)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	if err := srv.Start(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

// setupCacheMiddleware constructs the cache middleware. Returns a no-op
// middleware, a nil MemoryCache and a no-op closer when caching is disabled
// so callers can defer the closer unconditionally.
func setupCacheMiddleware(cfg *config.Config, registry *pii.Registry) (proxy.Middleware, *cache.MemoryCache, func()) {
	if !cfg.Cache.Enabled {
		slog.Info("cache disabled")
		return func(next http.Handler) http.Handler { return next }, nil, func() {}
	}
	memCache := cache.NewMemoryCache(cfg.Cache.MaxMemory)
	tc := cache.NewTierClassifier(registry.ContainsPII)
	mw := cache.CacheMiddleware(memCache, tc, &cfg.Cache)
	slog.Info("cache enabled", "maxMemory", cfg.Cache.MaxMemory, "piiChecker", "active")
	return mw, memCache, func() { memCache.Close() }
}

// startBodyRotator spawns the background body-rotation goroutine. The rotator
// shares the body store's backend so both observe the same object inventory.
func startBodyRotator(ctx context.Context, cfg *config.Config, bodyBackend blob.Backend, currentDir string, metaStore storage.Store) {
	recentMaxAge, _ := time.ParseDuration(cfg.Bodies.RecentMaxAge)
	archiveMaxAge, _ := time.ParseDuration(cfg.Bodies.ArchiveMaxAge)
	codec, err := bodies.NewCodec(cfg.Bodies.Compression)
	if err != nil {
		slog.Error("invalid bodies compression codec", "error", err)
		os.Exit(1)
	}
	rotator := bodies.NewRotator(bodyBackend, currentDir, bodies.RotatorOptions{
		Codec:          codec,
		RecentMaxAge:   recentMaxAge,
		ArchiveMaxAge:  archiveMaxAge,
		MaxBytes:       cfg.Bodies.MaxBytes,
		OrphanNotifier: storeOrphanNotifier{store: metaStore},
	})
	go rotator.Run(ctx)
}

// setupPrometheus initializes Prometheus metrics + collectors. Returns nil
// metrics and a no-op stopper when Prometheus is disabled so callers can
// defer the stopper unconditionally.
func setupPrometheus(cfg *config.Config, memCache *cache.MemoryCache, limiter *ratelimit.Limiter) (*prommetrics.Metrics, func()) {
	if !cfg.Prometheus.Enabled {
		return nil, func() {}
	}
	m := prommetrics.New(nil)
	var cacheProvider prommetrics.CacheStatsProvider
	if memCache != nil {
		cacheProvider = memCache
	}
	stop := prommetrics.StartCollectors(m, cacheProvider, limiter, 15*time.Second)
	slog.Info("prometheus metrics enabled", "path", cfg.Prometheus.Path, "port", cfg.Prometheus.Port)
	return m, stop
}

// setupRDTMiddleware returns the per-region RDT middleware factory. When
// auto-mint is disabled the factory yields a no-op middleware so the proxy
// chain stays uniform.
func setupRDTMiddleware(cfg *config.Config) func(server.Region) proxy.Middleware {
	if !cfg.RDT.AutoMint {
		slog.Info("rdt auto-mint disabled")
		return func(_ server.Region) proxy.Middleware {
			return func(next http.Handler) http.Handler { return next }
		}
	}
	rdtCache := rdt.NewCache(5 * time.Minute)
	reportTracker := rdt.NewReportTracker(70 * time.Minute) // reports can take up to 60min + margin
	slog.Info("rdt auto-mint enabled")
	return func(region server.Region) proxy.Middleware {
		host := server.RegionEndpoints[region]
		minter := rdt.NewMinter("https://"+host, &http.Client{Timeout: 10 * time.Second})
		mw := rdt.NewMiddleware(rdtCache, minter, reportTracker)
		return mw.Handler
	}
}

// warnIfStaticIAMKey logs and audits a warning when the configured S3 access
// key looks like a long-lived AKIA... IAM-user key and SSE is unset (F-21).
// The proxy never persists the key, but pairing a static credential with
// plaintext storage compounds the blast radius if the host is compromised.
func warnIfStaticIAMKey(ctx context.Context, cfg *config.Config, auditLogger *audit.AuditLogger) {
	matched, _ := regexp.MatchString(`^AKIA[A-Z0-9]{16}$`, cfg.Bodies.S3.AccessKey)
	if !matched || cfg.Bodies.S3.SSE != "" {
		return
	}
	w := "SP_PROXY_S3_ACCESS_KEY looks like a long-lived IAM user key (AKIA...) and SP_PROXY_S3_SSE is empty. Migrate to STS / role assumption and set SP_PROXY_S3_SSE=AES256 (or aws:kms) to enforce server-side encryption."
	slog.Warn("configuration warning", "message", w)
	_ = auditLogger.Log(ctx, audit.EventDPPComplianceWarning, "config", w, nil)
}

// mountMetricsHandler attaches the Prometheus /metrics endpoint either to
// the dashboard mux (shared port) or to its own listener. The handler is
// mounted before the security-headers middleware so scrapers don't pay the
// CSP/cache-control overhead (F-03).
func mountMetricsHandler(cfg *config.Config, dashMux *http.ServeMux) {
	if !cfg.Prometheus.Enabled {
		return
	}
	if cfg.Prometheus.Port == 0 {
		dashMux.Handle(cfg.Prometheus.Path, promhttp.Handler())
		return
	}
	metricsMux := http.NewServeMux()
	metricsMux.Handle(cfg.Prometheus.Path, promhttp.Handler())
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Prometheus.Port)
		slog.Info("prometheus metrics server starting", "addr", addr)
		if err := http.ListenAndServe(addr, metricsMux); err != nil {
			slog.Error("prometheus metrics server error", "error", err)
		}
	}()
}

// regionFactoryDeps bundles the wiring shared across regional proxy handlers.
type regionFactoryDeps struct {
	limiter         *ratelimit.Limiter
	asyncLogger     *logging.AsyncLogger
	registry        *pii.Registry
	resolver        *merchant.Resolver
	cacheMiddleware proxy.Middleware
	rlMiddleware    proxy.Middleware
	rdtMiddlewareFn func(server.Region) proxy.Middleware
	promMetrics     *prommetrics.Metrics
}

// buildRegionFactory returns the per-region handler factory that the server
// uses to construct one proxy chain per configured region.
func buildRegionFactory(cfg *config.Config, d regionFactoryDeps) func(server.Region) http.Handler {
	return func(region server.Region) http.Handler {
		rp, err := proxy.NewRegionProxyWithLimiter(region, d.limiter)
		if err != nil {
			slog.Error("failed to create proxy", "region", region, "error", err)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "proxy misconfigured", http.StatusInternalServerError)
			})
		}
		logMiddleware := logging.LoggingMiddleware(d.asyncLogger, d.registry, string(region), cfg.Bodies.MaxCaptureSize)
		rdtMw := d.rdtMiddlewareFn(region)
		middlewares := []proxy.Middleware{d.resolver.Middleware(), logMiddleware, rdtMw, d.cacheMiddleware, d.rlMiddleware}
		if d.promMetrics != nil {
			// Prometheus middleware is outermost - wraps everything including logging.
			middlewares = append([]proxy.Middleware{prommetrics.Middleware(d.promMetrics, string(region))}, middlewares...)
		}
		return proxy.BuildChain(rp, middlewares...)
	}
}

// newBodyBackend constructs the blob.Backend selected by
// SP_PROXY_BODIES_BACKEND. Local is the default; s3 uses the S3Config block.
func newBodyBackend(ctx context.Context, cfg *config.Config) (blob.Backend, error) {
	switch cfg.Bodies.Backend {
	case "", "local":
		return blob.NewLocal(cfg.Bodies.BasePath)
	case "s3":
		return blob.NewS3(ctx, blob.S3Options{
			Bucket:      cfg.Bodies.S3.Bucket,
			Region:      cfg.Bodies.S3.Region,
			Endpoint:    cfg.Bodies.S3.Endpoint,
			AccessKey:   cfg.Bodies.S3.AccessKey,
			SecretKey:   cfg.Bodies.S3.SecretKey,
			PathStyle:   cfg.Bodies.S3.PathStyle,
			SSE:         cfg.Bodies.S3.SSE,
			SSEKMSKeyID: cfg.Bodies.S3.SSEKMSKey,
		})
	default:
		return nil, fmt.Errorf("unsupported bodies backend: %s", cfg.Bodies.Backend)
	}
}

// storeOrphanNotifier adapts a storage.Store to bodies.OrphanNotifier so the
// rotator can null dangling body pointers when it deletes objects.
type storeOrphanNotifier struct {
	store storage.Store
}

func (s storeOrphanNotifier) OnBodiesDeleted(ctx context.Context, files []string) error {
	n, err := s.store.NullifyBodyRefs(ctx, files)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Info("rotator: nulled body refs", "count", n, "files", len(files))
	}
	return nil
}
