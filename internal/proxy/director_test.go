package proxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirector_SetsSchemeAndHost(t *testing.T) {
	dir := newDirector("sellingpartnerapi-eu.amazon.com")
	req, _ := http.NewRequest("GET", "http://localhost:8080/orders/v0/orders", nil)

	dir(req)

	assert.Equal(t, "https", req.URL.Scheme)
	assert.Equal(t, "sellingpartnerapi-eu.amazon.com", req.URL.Host)
	assert.Equal(t, "sellingpartnerapi-eu.amazon.com", req.Host)
}

func TestDirector_PreservesPath(t *testing.T) {
	dir := newDirector("sellingpartnerapi-na.amazon.com")
	req, _ := http.NewRequest("GET", "http://localhost:8081/orders/v0/orders?MarketplaceIds=ATVPDKIKX0DER", nil)

	dir(req)

	assert.Equal(t, "/orders/v0/orders", req.URL.Path)
	assert.Equal(t, "ATVPDKIKX0DER", req.URL.Query().Get("MarketplaceIds"))
}

func TestDirector_StripsProxyHeaders(t *testing.T) {
	dir := newDirector("sellingpartnerapi-eu.amazon.com")
	req, _ := http.NewRequest("GET", "http://localhost:8080/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Merchant-Id", "my-merchant")
	req.Header.Set("X-SP-Proxy-Cache-TTL", "300")
	req.Header.Set("X-SP-Proxy-No-Cache", "true")
	req.Header.Set("X-Amz-Access-Token", "Atza|secret")

	dir(req)

	assert.Empty(t, req.Header.Get("X-SP-Proxy-Merchant-Id"), "proxy headers must be stripped")
	assert.Empty(t, req.Header.Get("X-SP-Proxy-Cache-TTL"), "proxy headers must be stripped")
	assert.Empty(t, req.Header.Get("X-SP-Proxy-No-Cache"), "proxy headers must be stripped")
	assert.Equal(t, "Atza|secret", req.Header.Get("X-Amz-Access-Token"), "Amazon headers must be preserved")
}

func TestDirector_PreservesAllAmazonHeaders(t *testing.T) {
	dir := newDirector("sellingpartnerapi-fe.amazon.com")
	req, _ := http.NewRequest("GET", "http://localhost:8082/catalog/v0/items", nil)
	req.Header.Set("X-Amz-Access-Token", "Atza|token123")
	req.Header.Set("X-Amz-Date", "20260325T120000Z")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=...")
	req.Header.Set("User-Agent", "my-app/1.0")

	dir(req)

	assert.Equal(t, "Atza|token123", req.Header.Get("X-Amz-Access-Token"))
	assert.Equal(t, "20260325T120000Z", req.Header.Get("X-Amz-Date"))
	assert.Equal(t, "AWS4-HMAC-SHA256 Credential=...", req.Header.Get("Authorization"))
	assert.Equal(t, "my-app/1.0", req.Header.Get("User-Agent"))
}

func TestDirector_CaseInsensitiveProxyHeaderStrip(t *testing.T) {
	dir := newDirector("sellingpartnerapi-eu.amazon.com")
	req, _ := http.NewRequest("GET", "http://localhost:8080/test", nil)
	req.Header.Set("x-sp-proxy-merchant-id", "test")
	req.Header.Set("X-Sp-Proxy-Priority", "high")

	dir(req)

	require.Empty(t, req.Header.Get("X-Sp-Proxy-Merchant-Id"))
	require.Empty(t, req.Header.Get("X-Sp-Proxy-Priority"))
}
