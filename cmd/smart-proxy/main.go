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
	var cacheMiddleware proxy.Middleware
	var memCache *cache.MemoryCache
	if cfg.Cache.Enabled {
		memCache = cache.NewMemoryCache(cfg.Cache.MaxMemory)
		defer memCache.Close()
		tc := cache.NewTierClassifier(registry.ContainsPII)
		cacheMiddleware = cache.CacheMiddleware(memCache, tc, &cfg.Cache)
		slog.Info("cache enabled", "maxMemory", cfg.Cache.MaxMemory, "piiChecker", "active")
	} else {
		cacheMiddleware = func(next http.Handler) http.Handler { return next }
		slog.Info("cache disabled")
	}

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

	// Body rotator (background). Reuses the body store's backend so both
	// see the same object inventory; local and S3 backends are safely shared.
	if cfg.Bodies.Enabled {
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

	// F-21 helper: nudge operators away from long-lived IAM-user keys for
	// the proxy's own S3 access. The proxy itself does not store the key
	// (it only holds it in-memory for the AWS SDK), but a static AKIA...
	// secret paired with a plaintext bucket compounds blast radius if the
	// host is compromised. STS / role assumption is the safer path; pair
	// it with SP_PROXY_S3_SSE so PutObject calls enforce server-side
	// encryption rather than relying on bucket-default behavior.
	if matched, _ := regexp.MatchString(`^AKIA[A-Z0-9]{16}$`, cfg.Bodies.S3.AccessKey); matched && cfg.Bodies.S3.SSE == "" {
		w := "SP_PROXY_S3_ACCESS_KEY looks like a long-lived IAM user key (AKIA...) and SP_PROXY_S3_SSE is empty. Migrate to STS / role assumption and set SP_PROXY_S3_SSE=AES256 (or aws:kms) to enforce server-side encryption."
		slog.Warn("configuration warning", "message", w)
		_ = auditLogger.Log(ctx, audit.EventDPPComplianceWarning, "config", w, nil)
	}

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

	// Prometheus metrics
	var promMetrics *prommetrics.Metrics
	if cfg.Prometheus.Enabled {
		promMetrics = prommetrics.New(nil)

		var cacheProvider prommetrics.CacheStatsProvider
		if memCache != nil {
			cacheProvider = memCache
		}
		stopCollectors := prommetrics.StartCollectors(promMetrics, cacheProvider, limiter, 15*time.Second)
		defer stopCollectors()
		slog.Info("prometheus metrics enabled", "path", cfg.Prometheus.Path, "port", cfg.Prometheus.Port)
	}

	// Dashboard handler
	dashHandler := dashboard.NewHandlerWithPII(metaStore, auditStore, bodyStore, piiEngine)
	dashMux := dashboard.NewMux(dashHandler)

	// Mount /metrics on dashboard mux (or separate port).
	// /metrics is mounted on the bare mux BEFORE the security-headers
	// wrapping (F-03) so Prometheus scrapes don't pay the CSP/cache-control
	// overhead. The security headers are harmless to scrapers, but adding
	// them to a metrics endpoint would mislead anyone debugging the headers
	// later about which endpoints are part of the dashboard vs the
	// observability surface.
	if cfg.Prometheus.Enabled {
		if cfg.Prometheus.Port == 0 {
			// Serve on dashboard port
			dashMux.Handle(cfg.Prometheus.Path, promhttp.Handler())
		} else {
			// Serve on separate port
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
	}

	// RDT auto-minting middleware
	var rdtCache *rdt.Cache
	var rdtMiddlewareFn func(server.Region) proxy.Middleware
	if cfg.RDT.AutoMint {
		rdtCache = rdt.NewCache(5 * time.Minute)
		reportTracker := rdt.NewReportTracker(70 * time.Minute) // reports can take up to 60min + margin
		rdtMiddlewareFn = func(region server.Region) proxy.Middleware {
			host := server.RegionEndpoints[region]
			minter := rdt.NewMinter("https://"+host, &http.Client{Timeout: 10 * time.Second})
			mw := rdt.NewMiddleware(rdtCache, minter, reportTracker)
			return mw.Handler
		}
		slog.Info("rdt auto-mint enabled")
	} else {
		rdtMiddlewareFn = func(_ server.Region) proxy.Middleware {
			return func(next http.Handler) http.Handler { return next }
		}
		slog.Info("rdt auto-mint disabled")
	}

	factory := func(region server.Region) http.Handler {
		rp, err := proxy.NewRegionProxyWithLimiter(region, limiter)
		if err != nil {
			slog.Error("failed to create proxy", "region", region, "error", err)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "proxy misconfigured", http.StatusInternalServerError)
			})
		}
		logMiddleware := logging.LoggingMiddleware(asyncLogger, registry, string(region), cfg.Bodies.MaxCaptureSize)
		rdtMw := rdtMiddlewareFn(region)
		middlewares := []proxy.Middleware{resolver.Middleware(), logMiddleware, rdtMw, cacheMiddleware, rlMiddleware}
		if promMetrics != nil {
			// Prometheus middleware is outermost  -  wraps everything including logging.
			middlewares = append([]proxy.Middleware{prommetrics.Middleware(promMetrics, string(region))}, middlewares...)
		}
		return proxy.BuildChain(rp, middlewares...)
	}

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
