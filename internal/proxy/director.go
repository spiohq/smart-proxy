package proxy

import (
	"net/http"
	"strings"
)

// newDirector returns an httputil.ReverseProxy Director function that rewrites
// the request to target the given SP-API host over HTTPS, and strips all
// X-SP-Proxy-* headers before forwarding to Amazon.
func newDirector(targetHost string) func(*http.Request) {
	return newDirectorWithScheme("https", targetHost)
}

// newDirectorWithScheme is like newDirector but allows specifying the scheme.
// Used in tests where the mock server uses plain HTTP.
func newDirectorWithScheme(scheme, targetHost string) func(*http.Request) {
	return func(req *http.Request) {
		req.URL.Scheme = scheme
		req.URL.Host = targetHost
		req.Host = targetHost

		// Strip all X-SP-Proxy-* headers  -  they are proxy-internal and must
		// never reach Amazon's servers.
		for key := range req.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-sp-proxy-") {
				req.Header.Del(key)
			}
		}
	}
}
