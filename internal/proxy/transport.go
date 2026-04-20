package proxy

import (
	"net/http"
	"time"
)

// newTransport returns an HTTP transport tuned for high-throughput proxying
// to Amazon's SP-API endpoints.
func newTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DisableCompression:    false,
	}
}
