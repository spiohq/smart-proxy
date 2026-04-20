package proxy

import (
	"fmt"
	"net/http/httputil"

	"github.com/spiohq/smart-proxy/internal/ratelimit"
	"github.com/spiohq/smart-proxy/internal/server"
)

// NewRegionProxy creates an httputil.ReverseProxy that forwards requests to
// the given SP-API region's endpoint. The proxy is wrapped with the provided
// middleware chain.
func NewRegionProxy(region server.Region) (*httputil.ReverseProxy, error) {
	target, ok := server.RegionEndpoints[region]
	if !ok {
		return nil, fmt.Errorf("unknown region: %s", region)
	}

	return &httputil.ReverseProxy{
		Director:       newDirector(target),
		ModifyResponse: newResponseModifier(),
		ErrorHandler:   newErrorHandler(),
		Transport:      newTransport(),
	}, nil
}

// NewTestProxy creates a proxy that forwards to the given host using plain HTTP.
// Only for testing  -  production always uses HTTPS.
func NewTestProxy(host string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director:       newDirectorWithScheme("http", host),
		ModifyResponse: newResponseModifier(),
		ErrorHandler:   newErrorHandler(),
	}
}

func NewRegionProxyWithLimiter(region server.Region, limiter *ratelimit.Limiter) (*httputil.ReverseProxy, error) {
	target, ok := server.RegionEndpoints[region]
	if !ok {
		return nil, fmt.Errorf("unknown region: %s", region)
	}

	return &httputil.ReverseProxy{
		Director:       newDirector(target),
		ModifyResponse: newResponseModifierWithLimiter(limiter),
		ErrorHandler:   newErrorHandler(),
		Transport:      newTransport(),
	}, nil
}

func NewTestProxyWithLimiter(host string, limiter *ratelimit.Limiter) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director:       newDirectorWithScheme("http", host),
		ModifyResponse: newResponseModifierWithLimiter(limiter),
		ErrorHandler:   newErrorHandler(),
	}
}
