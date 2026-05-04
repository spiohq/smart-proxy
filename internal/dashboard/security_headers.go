package dashboard

import "net/http"

// SecurityHeadersMiddleware sets the modern dashboard security-header set on
// every response. 'unsafe-inline' is required for both scripts and styles:
// SvelteKit emits a small bootstrap inline-script that cannot be hashed
// statically (its content includes a build-time fingerprint), and Tailwind
// requires inline styles. The dashboard is an internal tool not exposed to
// untrusted users, so the residual XSS risk is accepted. All other directives
// remain strict (Pentest finding F-03).
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"font-src 'self' data:; " +
		"img-src 'self' data:; " +
		"connect-src 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store, no-cache, must-revalidate")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}
