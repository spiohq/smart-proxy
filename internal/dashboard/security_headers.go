package dashboard

import "net/http"

// SecurityHeadersMiddleware sets the modern dashboard security-header set on
// every response. The CSP is tight enough to mitigate inline-script XSS even
// if a future bug leaks attacker-controlled content into the SPA;
// 'unsafe-inline' for styles is a Tailwind concession.
//
// Pentest finding F-03.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline'; " +
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
