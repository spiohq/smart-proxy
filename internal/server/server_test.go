package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
