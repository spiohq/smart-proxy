package dashboard

import (
	"net/http"

	"github.com/spiohq/smart-proxy/internal/audit"
	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/storage"
)

// Handler holds dependencies for all dashboard API endpoints.
type Handler struct {
	logStore   storage.Store
	auditStore audit.Store
	bodyStore  bodies.BodyStore
}

// NewHandler creates a dashboard handler with the given store dependencies.
func NewHandler(logStore storage.Store, auditStore audit.Store, bodyStore bodies.BodyStore) *Handler {
	return &Handler{
		logStore:   logStore,
		auditStore: auditStore,
		bodyStore:  bodyStore,
	}
}

// NewMux returns an http.ServeMux with all API routes and SPA serving registered.
func NewMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoints (existing)
	mux.HandleFunc("GET /_sp-proxy/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /_sp-proxy/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	// API v1 endpoints
	mux.HandleFunc("GET /api/v1/logs", h.handleLogs)
	mux.HandleFunc("GET /api/v1/logs/{id}", h.handleLogByID)
	mux.HandleFunc("GET /api/v1/logs/{id}/body", h.handleLogBody)
	mux.HandleFunc("GET /api/v1/audit", h.handleAudit)
	mux.HandleFunc("GET /api/v1/merchants", h.handleMerchants)
	// SPA catch-all (must be last)
	mux.Handle("/", spaHandler())

	return mux
}

