package proxy

import "net/http"

// Middleware wraps an http.Handler, adding behavior before/after the next handler.
type Middleware func(http.Handler) http.Handler

// BuildChain wraps the base handler with middlewares applied in order.
// The first middleware in the slice is the outermost (runs first).
func BuildChain(handler http.Handler, middlewares ...Middleware) http.Handler {
	// Apply in reverse so the first middleware is outermost
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
