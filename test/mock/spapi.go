package mock

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
)

// RecordedRequest captures a request received by the mock server.
type RecordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// Response defines what the mock server returns for a given path.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// MockSPAPI is a test HTTP server that simulates Amazon's SP-API.
type MockSPAPI struct {
	*httptest.Server
	mu           sync.Mutex
	Requests     []*RecordedRequest
	Responses    map[string]Response
	latencies    map[string]time.Duration
	requestCount map[string]int
}

// NewMockSPAPI creates a mock SP-API server with a default 200 response.
func NewMockSPAPI() *MockSPAPI {
	m := &MockSPAPI{
		Responses:    map[string]Response{},
		latencies:    map[string]time.Duration{},
		requestCount: map[string]int{},
	}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)

		m.mu.Lock()
		m.Requests = append(m.Requests, &RecordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   body,
		})
		m.requestCount[r.URL.Path]++

		// Capture latency and response before releasing the mutex.
		// Query-aware matching: try path+query first, then path-only.
		latency := m.latencies[r.URL.Path]
		resp, hasResp := m.Responses[r.URL.Path+"?"+r.URL.RawQuery]
		if !hasResp {
			resp, hasResp = m.Responses[r.URL.Path]
		}
		m.mu.Unlock()

		// Sleep after releasing mutex, before writing the response.
		if latency > 0 {
			time.Sleep(latency)
		}

		if hasResp {
			for k, vs := range resp.Headers {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			w.Write(resp.Body)
			return
		}

		// Default: 200 with empty JSON body and realistic headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-amzn-RequestId", "mock-request-id-12345")
		w.Header().Set("x-amzn-RateLimit-Limit", "0.0167")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"payload":{}}`))
	}))
	return m
}

// SetResponse configures a specific response for a path.
// The path can include a query string (e.g. "/catalog/v0/items?includedData=attributes")
// for query-aware matching. Exact path+query matches take priority over path-only matches.
func (m *MockSPAPI) SetResponse(path string, resp Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Responses[path] = resp
}

// SetLatency configures a simulated latency for a path.
func (m *MockSPAPI) SetLatency(path string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies[path] = d
}

// RequestCount returns the number of requests received for a path.
func (m *MockSPAPI) RequestCount(path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requestCount[path]
}

// SetError configures the mock to return an HTTP error for the given path.
func (m *MockSPAPI) SetError(path string, code int) {
	m.SetResponse(path, Response{
		StatusCode: code,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"errors":[{"code":"MockError","message":"injected error"}]}`),
	})
}

// LastRequest returns the most recently recorded request, or nil.
func (m *MockSPAPI) LastRequest() *RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Requests) == 0 {
		return nil
	}
	return m.Requests[len(m.Requests)-1]
}

// FindRequest returns the first recorded request matching the given path, or nil.
func (m *MockSPAPI) FindRequest(path string) *RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.Requests {
		if r.Path == path {
			return r
		}
	}
	return nil
}

// Reset clears all recorded requests and request counts.
func (m *MockSPAPI) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = nil
	m.requestCount = map[string]int{}
}

// FindRequests returns all recorded requests matching the given path.
func (m *MockSPAPI) FindRequests(path string) []*RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*RecordedRequest
	for _, r := range m.Requests {
		if r.Path == path {
			result = append(result, r)
		}
	}
	return result
}

// AllRequests returns a copy of all recorded requests.
func (m *MockSPAPI) AllRequests() []*RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*RecordedRequest, len(m.Requests))
	copy(result, m.Requests)
	return result
}

// TotalRequestCount returns the total number of requests received across all paths.
func (m *MockSPAPI) TotalRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Requests)
}
