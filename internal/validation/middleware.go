package validation

import (
	"net/http"
	"sync/atomic"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
)

// Middleware is a standard HTTP middleware: it wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// AtomicRouter holds a Router that can be swapped atomically (e.g. on spec
// refresh). atomic.Value requires a concrete type, so we store the interface
// value directly; callers must never store an untyped nil.
type AtomicRouter struct {
	val atomic.Value // stores routers.Router (interface)
}

// Store saves r as the current router. Passing nil is a no-op; the previous
// value (or no-op state) is preserved.
func (a *AtomicRouter) Store(r Router) {
	if r == nil {
		return
	}
	a.val.Store(r)
}

// Load returns the current router, or nil if none has been stored.
func (a *AtomicRouter) Load() Router {
	v := a.val.Load()
	if v == nil {
		return nil
	}
	return v.(Router)
}

// NewMiddleware returns a Middleware that validates incoming requests against
// the given router. If router is nil the middleware is a no-op (passes every
// request through).
func NewMiddleware(router Router) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if router == nil {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("X-SP-Proxy-Skip-Validation") == "true" {
				next.ServeHTTP(w, r)
				return
			}
			if err := validate(router, r); err != nil {
				writeValidationError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NewMiddlewareFromAtomic returns a Middleware that reads the router from ar on
// every request, enabling hot-swap without restarting the server.
func NewMiddlewareFromAtomic(ar *AtomicRouter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			router := ar.Load()
			if router == nil {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("X-SP-Proxy-Skip-Validation") == "true" {
				next.ServeHTTP(w, r)
				return
			}
			if err := validate(router, r); err != nil {
				writeValidationError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// validate runs OpenAPI request validation against router. It returns nil when
// the route is not found (graceful degradation for unknown endpoints).
func validate(router Router, r *http.Request) error {
	route, pathParams, err := router.FindRoute(r)
	if err != nil {
		if err == routers.ErrPathNotFound || err == routers.ErrMethodNotAllowed {
			return nil
		}
		return nil
	}

	input := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			MultiError: true,
		},
	}
	return openapi3filter.ValidateRequest(r.Context(), input)
}

// writeValidationError sends a 400 Bad Request with SP-API-style JSON body.
func writeValidationError(w http.ResponseWriter, err error) {
	body := FormatValidationErrors(err)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-SP-Proxy-Validation", "rejected")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(body)
}
