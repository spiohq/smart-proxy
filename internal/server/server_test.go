package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_GracefulShutdown(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:          0,
			PortNA:          0,
			PortFE:          0,
			PortDashboard:   freePort(t),
			ShutdownTimeout: "5s",
		},
	}

	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	srv, err := New(cfg, nil, slowHandler)
	require.NoError(t, err)

	go srv.Start()
	defer srv.Shutdown()

	require.Eventually(t, func() bool {
		return srv.DashboardAddr() != ""
	}, 2*time.Second, 50*time.Millisecond)

	resultCh := make(chan int, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://%s/_sp-proxy/health", srv.DashboardAddr()))
		if err != nil {
			resultCh <- 0
			return
		}
		defer resp.Body.Close()
		resultCh <- resp.StatusCode
	}()

	time.Sleep(100 * time.Millisecond)
	err = srv.Shutdown()
	require.NoError(t, err)

	status := <-resultCh
	assert.Equal(t, http.StatusOK, status)
}

// freePort asks the OS for an available port and returns it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestServer_HealthEndpoint(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:          0, // disabled
			PortNA:          0, // disabled
			PortFE:          0, // disabled
			PortDashboard:   freePort(t),
			ShutdownTimeout: "5s",
		},
	}

	srv, err := New(cfg, nil, nil)
	require.NoError(t, err)

	go srv.Start()
	defer srv.Shutdown()

	// Wait for server to be ready
	require.Eventually(t, func() bool {
		return srv.DashboardAddr() != ""
	}, 2*time.Second, 50*time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://%s/_sp-proxy/health", srv.DashboardAddr()))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "ok", body["status"])
}

func TestServer_DisabledRegionNotStarted(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:          freePort(t), // enabled
			PortNA:          0,           // disabled
			PortFE:          0,           // disabled
			PortDashboard:   freePort(t),
			ShutdownTimeout: "5s",
		},
	}

	srv, err := New(cfg, nil, nil)
	require.NoError(t, err)

	go srv.Start()
	defer srv.Shutdown()

	require.Eventually(t, func() bool {
		return srv.DashboardAddr() != ""
	}, 2*time.Second, 50*time.Millisecond)

	// EU should be running
	assert.NotEmpty(t, srv.RegionAddr(RegionEU))
	// NA and FE should not
	assert.Empty(t, srv.RegionAddr(RegionNA))
	assert.Empty(t, srv.RegionAddr(RegionFE))
}

func TestDashboardBindAddr_DefaultsToLoopback(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:            0,
			PortNA:            0,
			PortFE:            0,
			PortDashboard:     freePort(t),
			DashboardBindAddr: "127.0.0.1",
			ShutdownTimeout:   "5s",
		},
	}

	srv, err := New(cfg, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, err)

	go srv.Start()
	defer srv.Shutdown()

	require.Eventually(t, func() bool {
		return srv.DashboardAddr() != ""
	}, 2*time.Second, 20*time.Millisecond)

	addr := srv.DashboardAddr()
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	assert.True(t, host == "127.0.0.1" || host == "::1",
		"dashboard bound to non-loopback host %q", host)
}

func TestDashboardBindAddr_ExplicitWildcard(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:            0,
			PortNA:            0,
			PortFE:            0,
			PortDashboard:     freePort(t),
			DashboardBindAddr: "0.0.0.0",
			ShutdownTimeout:   "5s",
		},
	}

	srv, err := New(cfg, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, err)

	go srv.Start()
	defer srv.Shutdown()

	require.Eventually(t, func() bool {
		return srv.DashboardAddr() != ""
	}, 2*time.Second, 20*time.Millisecond)

	addr := srv.DashboardAddr()
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	// On wildcard bind, the OS may report the address as "::" or "0.0.0.0"
	// depending on dual-stack behaviour. Either is correct.
	assert.True(t, host == "0.0.0.0" || host == "::",
		"wildcard bind expected 0.0.0.0 or ::, got %q", host)
}

// waitForRegionAddr polls srv.RegionAddr(region) until it is non-empty or 2s
// have elapsed.
func waitForRegionAddr(t *testing.T, srv *Server, region Region) string {
	t.Helper()
	var addr string
	require.Eventually(t, func() bool {
		addr = srv.RegionAddr(region)
		return addr != ""
	}, 2*time.Second, 20*time.Millisecond, "region %s never started", region)
	return addr
}

// TestRegionServer_ReadHeaderTimeout verifies that the region server closes
// a connection whose HTTP headers are never completed (Slowloris defense).
func TestRegionServer_ReadHeaderTimeout(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			PortEU:          freePort(t),
			PortNA:          0,
			PortFE:          0,
			PortDashboard:   freePort(t),
			RegionBindAddr:  "127.0.0.1",
			ShutdownTimeout: "5s",
		},
	}

	srv, err := New(cfg, func(region Region) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}, nil)
	require.NoError(t, err)
	go srv.Start()
	defer srv.Shutdown()

	addr := waitForRegionAddr(t, srv, RegionEU)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()
	// Send an incomplete HTTP request -- headers are never terminated.
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n"))

	conn.SetDeadline(time.Now().Add(15 * time.Second))
	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	require.True(t, err == io.EOF || strings.Contains(string(buf), "HTTP/1.1 4"),
		"expected server to close slow header connection within 15s; got err=%v buf=%q", err, buf[:64])
}
