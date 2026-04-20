package pii

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRegistry_HasDefaultRules(t *testing.T) {
	reg := NewRegistry()

	rules := reg.RulesFor("/orders/v0/orders")
	assert.NotEmpty(t, rules, "expected rules for /orders/v0/orders")

	rules = reg.RulesFor("/shipping/v2/shipments")
	assert.NotEmpty(t, rules, "expected rules for /shipping/v2/shipments")

	// v2026 endpoints have conditional rules (returned by RulesFor)
	rules = reg.RulesFor("/orders/2026-01-01/orders")
	assert.NotEmpty(t, rules, "expected conditional rules for /orders/2026-01-01/orders")

	rules = reg.RulesFor("/orders/2026-01-01/orders/{orderId}")
	assert.NotEmpty(t, rules, "expected conditional rules for /orders/2026-01-01/orders/{orderId}")
}

func TestRegistry_RulesFor_UnknownEndpoint(t *testing.T) {
	reg := NewRegistry()

	rules := reg.RulesFor("/unknown/v1/endpoint")
	assert.Empty(t, rules, "expected no rules for unknown endpoint")
}

func TestRegistry_IsFullBodyPII(t *testing.T) {
	reg := NewRegistry()

	truePatterns := []string{
		"/orders/v0/orders/{orderId}/buyerInfo",
		"/orders/v0/orders/{orderId}/address",
		"/orders/v0/orders/{orderId}/orderItems/buyerInfo",
		"/messaging/v1/orders/{orderId}/messages/{messageId}",
	}
	for _, p := range truePatterns {
		assert.True(t, reg.IsFullBodyPII(p), "expected IsFullBodyPII=true for %s", p)
	}

	falsePatterns := []string{
		"/orders/v0/orders",
		"/catalog/2022-04-01/items",
	}
	for _, p := range falsePatterns {
		assert.False(t, reg.IsFullBodyPII(p), "expected IsFullBodyPII=false for %s", p)
	}
}

func TestContainsPII_NonGET_ReturnsFalse(t *testing.T) {
	reg := NewRegistry()

	r := &http.Request{
		Method: http.MethodPost,
		URL:    mustParseURL("/orders/v0/orders/123-456/buyerInfo"),
	}
	assert.False(t, reg.ContainsPII(r))
}

func TestContainsPII_FullBodyPIIEndpoint(t *testing.T) {
	reg := NewRegistry()

	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/orders/v0/orders/123-456/buyerInfo"),
	}
	assert.True(t, reg.ContainsPII(r))
}

func TestContainsPII_EndpointWithPIIRules(t *testing.T) {
	reg := NewRegistry()

	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/orders/v0/orders"),
	}
	assert.True(t, reg.ContainsPII(r))
}

func TestContainsPII_DataElementsQueryParam(t *testing.T) {
	reg := NewRegistry()

	tests := []struct {
		name        string
		queryString string
		want        bool
	}{
		{
			name:        "buyerInfo only",
			queryString: "dataElements=buyerInfo",
			want:        true,
		},
		{
			name:        "shippingAddress only",
			queryString: "dataElements=shippingAddress",
			want:        true,
		},
		{
			name:        "both comma-separated",
			queryString: "dataElements=buyerInfo,shippingAddress",
			want:        true,
		},
		{
			name:        "orderStatus only",
			queryString: "dataElements=orderStatus",
			want:        false,
		},
		{
			name:        "no dataElements param",
			queryString: "",
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rawURL := "/catalog/2022-04-01/items"
			if tc.queryString != "" {
				rawURL += "?" + tc.queryString
			}
			r := &http.Request{
				Method: http.MethodGet,
				URL:    mustParseURL(rawURL),
			}
			assert.Equal(t, tc.want, reg.ContainsPII(r))
		})
	}
}

func TestContainsPII_IncludedDataQueryParam(t *testing.T) {
	reg := NewRegistry()

	tests := []struct {
		name        string
		path        string
		queryString string
		want        bool
	}{
		{
			name:        "v2026 orders with BUYER",
			path:        "/orders/2026-01-01/orders",
			queryString: "includedData=BUYER",
			want:        true,
		},
		{
			name:        "v2026 orders with RECIPIENT",
			path:        "/orders/2026-01-01/orders",
			queryString: "includedData=RECIPIENT",
			want:        true,
		},
		{
			name:        "v2026 orders with both comma-separated",
			path:        "/orders/2026-01-01/orders",
			queryString: "includedData=BUYER,RECIPIENT",
			want:        true,
		},
		{
			name:        "v2026 orders with PII among non-PII",
			path:        "/orders/2026-01-01/orders",
			queryString: "includedData=FULFILLMENT,BUYER,PROCEEDS",
			want:        true,
		},
		{
			name:        "v2026 single order with BUYER",
			path:        "/orders/2026-01-01/orders/902-1234567-1234567",
			queryString: "includedData=BUYER",
			want:        true,
		},
		{
			name:        "v2026 single order with RECIPIENT",
			path:        "/orders/2026-01-01/orders/902-1234567-1234567",
			queryString: "includedData=RECIPIENT",
			want:        true,
		},
		{
			name:        "v2026 orders with non-PII includedData only",
			path:        "/orders/2026-01-01/orders",
			queryString: "includedData=FULFILLMENT,PROCEEDS",
			want:        false,
		},
		{
			name:        "any endpoint with includedData=BUYER",
			path:        "/catalog/2022-04-01/items",
			queryString: "includedData=BUYER",
			want:        true,
		},
		{
			name:        "non-PII includedData on unrelated endpoint",
			path:        "/catalog/2022-04-01/items",
			queryString: "includedData=summaries",
			want:        false,
		},
		{
			name:        "no includedData param",
			path:        "/catalog/2022-04-01/items",
			queryString: "",
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rawURL := tc.path
			if tc.queryString != "" {
				rawURL += "?" + tc.queryString
			}
			r := &http.Request{
				Method: http.MethodGet,
				URL:    mustParseURL(rawURL),
			}
			assert.Equal(t, tc.want, reg.ContainsPII(r))
		})
	}
}

func TestContainsPII_V2026OrdersWithoutPIIParams(t *testing.T) {
	reg := NewRegistry()

	// v2026 orders WITHOUT includedData=BUYER/RECIPIENT should NOT be
	// flagged as PII  -  the response won't contain buyer/recipient data.
	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/orders/2026-01-01/orders"),
	}
	assert.False(t, reg.ContainsPII(r))

	r = &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/orders/2026-01-01/orders/902-1234567-1234567"),
	}
	assert.False(t, reg.ContainsPII(r))

	// But WITH includedData=BUYER it should be true
	r = &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/orders/2026-01-01/orders?includedData=BUYER"),
	}
	assert.True(t, reg.ContainsPII(r))
}

func TestRegistry_RulesFor_IncludesConditional(t *testing.T) {
	reg := NewRegistry()

	// v2026 endpoints should return conditional rules via RulesFor
	// (used at log-redaction time, when response body is present).
	rules := reg.RulesFor("/orders/2026-01-01/orders")
	assert.NotEmpty(t, rules, "expected conditional rules for v2026 orders list")

	rules = reg.RulesFor("/orders/2026-01-01/orders/{orderId}")
	assert.NotEmpty(t, rules, "expected conditional rules for v2026 single order")

	// Verify specific field paths
	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	assert.Contains(t, paths, "$.buyer.buyerEmail")
	assert.Contains(t, paths, "$.recipient.deliveryAddress.name")
	assert.Contains(t, paths, "$.recipient.deliveryPreference.dropOffLocation")
}

func TestContainsPII_SafeEndpoint_ReturnsFalse(t *testing.T) {
	reg := NewRegistry()

	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/catalog/2022-04-01/items"),
	}
	assert.False(t, reg.ContainsPII(r))
}

func TestContainsPII_SafeEndpointWithAddress(t *testing.T) {
	reg := NewRegistry()

	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/orders/v0/orders/123-456/address"),
	}
	assert.True(t, reg.ContainsPII(r))
}

// mustParseURL parses a URL string and panics on error.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

