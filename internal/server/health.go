package server

import "net/http"

// NewHealthHandler returns an HTTP handler that serves health/readiness endpoints
// on the dashboard port.
func NewHealthHandler() http.Handler {
	mux := http.NewServeMux()

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

	return mux
}
