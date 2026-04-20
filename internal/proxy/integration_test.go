package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spiohq/smart-proxy/internal/proxy"
	"github.com/spiohq/smart-proxy/test/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_UpstreamUnreachable(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	host := mockAPI.Listener.Addr().String()
	mockAPI.Close()

	rp := proxy.NewTestProxy(host)
	handler := proxy.BuildChain(rp)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	rp := proxy.NewTestProxy(mockAPI.Listener.Addr().String())
	handler := proxy.BuildChain(rp)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	const numRequests = 50

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/orders/v0/orders", nil)
			handler.ServeHTTP(w, r)
			if w.Code == http.StatusOK {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(numRequests), successCount.Load())
	assert.Equal(t, numRequests, mockAPI.RequestCount("/orders/v0/orders"))
}

func TestIntegration_UpstreamErrorForwarded(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	mockAPI.SetError("/orders/v0/orders", 503)

	rp := proxy.NewTestProxy(mockAPI.Listener.Addr().String())
	handler := proxy.BuildChain(rp)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	handler.ServeHTTP(w, r)

	assert.Equal(t, 503, w.Code)
	body, _ := io.ReadAll(w.Result().Body)
	assert.Contains(t, string(body), "MockError")
}

func TestIntegration_RequestHeadersForwarded(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	rp := proxy.NewTestProxy(mockAPI.Listener.Addr().String())
	handler := proxy.BuildChain(rp)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/orders/v0/orders", nil)
	r.Header.Set("x-amz-access-token", "test-token-123")
	handler.ServeHTTP(w, r)

	assert.Equal(t, 200, w.Code)
	last := mockAPI.LastRequest()
	require.NotNil(t, last)
	assert.Equal(t, "test-token-123", last.Header.Get("x-amz-access-token"))
}

func TestIntegration_ResponseHeadersForwarded(t *testing.T) {
	mockAPI := mock.NewMockSPAPI()
	defer mockAPI.Close()

	mockAPI.SetResponse("/test", mock.Response{
		StatusCode: 200,
		Headers: http.Header{
			"x-amzn-RequestId":       []string{"req-abc"},
			"x-amzn-RateLimit-Limit": []string{"0.5"},
			"Content-Type":           []string{"application/json"},
		},
		Body: []byte(`{}`),
	})

	rp := proxy.NewTestProxy(mockAPI.Listener.Addr().String())
	handler := proxy.BuildChain(rp)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	handler.ServeHTTP(w, r)

	assert.Equal(t, "req-abc", w.Header().Get("x-amzn-RequestId"))
	assert.Equal(t, "0.5", w.Header().Get("x-amzn-RateLimit-Limit"))
}
