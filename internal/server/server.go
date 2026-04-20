package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
)

// HandlerFactory creates an HTTP handler for a given region.
// Each region gets its own handler (with its own reverse proxy pointed at
// the correct Amazon endpoint).
type HandlerFactory func(region Region) http.Handler

// Server manages multiple HTTP listeners: one per SP-API region + dashboard.
type Server struct {
	config           *config.Config
	handlerFactory   HandlerFactory
	dashboardHandler http.Handler
	regionServers    map[Region]*http.Server
	regionAddrs      map[Region]string
	dashboardAddr    string
	dashboardServer  *http.Server
	mu               sync.RWMutex
	shutdownOnce     sync.Once
}

// New creates a new Server. factory creates the HTTP handler for each region.
// Pass nil for dashboard-only mode (all proxy ports serve 404).
func New(cfg *config.Config, factory HandlerFactory, dashboardHandler http.Handler) (*Server, error) {
	if dashboardHandler == nil {
		dashboardHandler = NewHealthHandler()
	}
	return &Server{
		config:           cfg,
		handlerFactory:   factory,
		dashboardHandler: dashboardHandler,
		regionServers:    make(map[Region]*http.Server),
		regionAddrs:      make(map[Region]string),
	}, nil
}

// Start begins listening on all configured ports. Blocks until Shutdown
// is called or an OS signal is received.
func (s *Server) Start() error {
	sc := s.config.Server

	regionPorts := map[Region]int{
		RegionEU: sc.PortEU,
		RegionNA: sc.PortNA,
		RegionFE: sc.PortFE,
	}

	for _, region := range AllRegions() {
		port := regionPorts[region]
		if port <= 0 {
			continue // port 0 = disabled
		}

		var handler http.Handler
		if s.handlerFactory != nil {
			handler = s.handlerFactory(region)
		} else {
			handler = http.NotFoundHandler()
		}

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return fmt.Errorf("listen %s port %d: %w", region, port, err)
		}

		httpSrv := &http.Server{Handler: handler}

		s.mu.Lock()
		s.regionServers[region] = httpSrv
		s.regionAddrs[region] = ln.Addr().String()
		s.mu.Unlock()

		slog.Info("proxy started", "region", region, "addr", ln.Addr())
		go func(r Region, l net.Listener, srv *http.Server) {
			if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("proxy serve error", "region", r, "err", err)
			}
		}(region, ln, httpSrv)
	}

	// Dashboard
	dashLn, err := net.Listen("tcp", fmt.Sprintf(":%d", sc.PortDashboard))
	if err != nil {
		return fmt.Errorf("listen dashboard port %d: %w", sc.PortDashboard, err)
	}

	dashSrv := &http.Server{Handler: s.dashboardHandler}

	s.mu.Lock()
	s.dashboardServer = dashSrv
	s.dashboardAddr = dashLn.Addr().String()
	s.mu.Unlock()

	slog.Info("dashboard started", "addr", dashLn.Addr())
	go func() {
		if err := dashSrv.Serve(dashLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("dashboard serve error", "err", err)
		}
	}()

	// Block on signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down")
	return s.Shutdown()
}

// Shutdown gracefully stops all listeners, draining in-flight requests.
// It is idempotent: the second and subsequent calls are no-ops that return nil.
func (s *Server) Shutdown() error {
	var shutdownErr error
	s.shutdownOnce.Do(func() {
		timeout, err := time.ParseDuration(s.config.Server.ShutdownTimeout)
		if err != nil {
			timeout = 30 * time.Second
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		s.mu.RLock()
		servers := make([]*http.Server, 0, len(s.regionServers)+1)
		for _, srv := range s.regionServers {
			servers = append(servers, srv)
		}
		if s.dashboardServer != nil {
			servers = append(servers, s.dashboardServer)
		}
		s.mu.RUnlock()

		for _, srv := range servers {
			if err := srv.Shutdown(ctx); err != nil {
				slog.Error("shutdown error", "error", err)
				shutdownErr = err
			}
		}

		slog.Info("all servers stopped")
	})
	return shutdownErr
}

// DashboardAddr returns the dashboard listen address, or "" if not started.
func (s *Server) DashboardAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dashboardAddr
}

// RegionAddr returns the listen address for a region, or "" if disabled.
func (s *Server) RegionAddr(region Region) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.regionAddrs[region]
}
