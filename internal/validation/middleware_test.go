package validation_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spiohq/smart-proxy/internal/validation"
)

// buildTestRouter creates a router from a minimal inline spec for tests.
func buildTestRouter(t *testing.T) routers.Router {
	t.Helper()
	const spec = `{
	  "openapi": "3.0.0",
	  "info": {"title": "Test", "version": "1"},
	  "paths": {
	    "/orders/v0/orders": {
	      "get": {
	        "operationId": "getOrders",
	        "parameters": [
	          {"name": "marketplaceIds", "in": "query", "required": true, "schema": {"type": "string"}},
	          {"name": "pageSize", "in": "query", "required": false, "schema": {"type": "integer"}}
	        ],
	        "responses": {"200": {"description": "OK"}}
	      }
	    },
	    "/listings/2021-08-01/items/{sellerId}/{sku}": {
	      "put": {
	        "operationId": "putListingsItem",
	        "parameters": [
	          {"name": "sellerId", "in": "path", "required": true, "schema": {"type": "string"}},
	          {"name": "sku", "in": "path", "required": true, "schema": {"type": "string"}},
	          {"name": "marketplaceIds", "in": "query", "required": true, "schema": {"type": "string"}}
	        ],
	        "requestBody": {
	          "required": true,
	          "content": {
	            "application/json": {
	              "schema": {
	                "type": "object",
	                "required": ["productType"],
	                "properties": {
	                  "productType": {"type": "string"}
	                }
	              }
	            }
	          }
	        },
	        "responses": {"200": {"description": "OK"}}
	      }
	    }
	  }
	}`
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData([]byte(spec))
	require.NoError(t, err)
	require.NoError(t, doc.Validate(loader.Context))
	router, err := gorillamux.NewRouter(doc)
	require.NoError(t, err)
	return router
}

// passThroughHandler is a test handler that records whether it was called.
func passThroughHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddleware_NilRouter_PassesThrough(t *testing.T) {
	mw := validation.NewMiddleware(nil)
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_SkipHeader_PassesThrough(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders", nil)
	req.Header.Set("X-SP-Proxy-Skip-Validation", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("X-SP-Proxy-Validation"))
}

func TestMiddleware_UnknownPath_PassesThrough(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/someNewEndpoint/v99/items", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.True(t, called, "unknown paths must pass through (graceful degradation)")
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestMiddleware_MissingRequiredQueryParam_Returns400(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.False(t, called, "upstream must not be called on validation failure")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "rejected", rr.Header().Get("X-SP-Proxy-Validation"))
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	body, _ := io.ReadAll(rr.Body)
	assert.Contains(t, string(body), `"code":"InvalidInput"`)
	assert.Contains(t, string(body), "marketplaceIds")
}

func TestMiddleware_ValidRequest_PassesThrough(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders?marketplaceIds=ATVPDKIKX0DER", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("X-SP-Proxy-Validation"))
}

func TestMiddleware_WrongQueryParamType_Returns400(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders?marketplaceIds=A&pageSize=notanint", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "rejected", rr.Header().Get("X-SP-Proxy-Validation"))
}

func TestMiddleware_MissingRequiredBody_Returns400(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodPut,
		"/listings/2021-08-01/items/SELLER1/SKU1?marketplaceIds=A",
		nil,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMiddleware_WrongBodyFieldType_Returns400(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	body := strings.NewReader(`{"productType": 123}`)
	req := httptest.NewRequest(http.MethodPut,
		"/listings/2021-08-01/items/SELLER1/SKU1?marketplaceIds=A",
		body,
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMiddleware_MultipleErrorsAllReturned(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders?pageSize=notanint", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.False(t, called)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	body, _ := io.ReadAll(rr.Body)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "marketplaceIds")
	assert.Greater(t, strings.Count(bodyStr, `"code":"InvalidInput"`), 1)
}

func TestMiddleware_ValidationHeaderAbsentOnSuccess(t *testing.T) {
	mw := validation.NewMiddleware(buildTestRouter(t))
	called := false
	handler := mw(passThroughHandler(&called))
	req := httptest.NewRequest(http.MethodGet, "/orders/v0/orders?marketplaceIds=A", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.True(t, called)
	assert.Empty(t, rr.Header().Get("X-SP-Proxy-Validation"))
}
