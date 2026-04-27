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

// ── Fail-closed mode ──────────────────────────────────────────────

func TestNewRegistryFailClosed_FlagSet(t *testing.T) {
	reg := NewRegistryFailClosed()
	assert.True(t, reg.FailClosed())
}

func TestNewRegistry_FailClosedFlagDefaultsFalse(t *testing.T) {
	assert.False(t, NewRegistry().FailClosed())
}

func TestFailClosed_UnknownEndpointTreatedAsFullBodyPII(t *testing.T) {
	reg := NewRegistryFailClosed()
	// Unknown classification: Classify returns the path unchanged when no
	// pattern matches, and that path is in none of the rule maps.
	assert.True(t, reg.IsFullBodyPII("/futureapi/2030-01-01/unknown"))
}

func TestFailClosed_PatternNotInRuleMapsIsFullBody(t *testing.T) {
	// IsFullBodyPII operates on the classified pattern. In fail-closed mode,
	// any pattern that is in none of the rule maps (full-body, partial,
	// conditional) is reported as full-body. This includes both genuinely
	// unknown paths AND known SP-API endpoints that simply have no PII rules.
	// The latter is intentional: if we have not registered rules for an
	// endpoint, we cannot prove it does not return PII, so we redact its
	// body. Operators who want catalog/finances/etc readable in the
	// dashboard should leave fail-closed off, or register stub rules.
	reg := NewRegistryFailClosed()
	assert.True(t, reg.IsFullBodyPII("/catalog/2022-04-01/items"))
	assert.True(t, reg.IsFullBodyPII("/futureapi/2030-01-01/unknown"))
}

func TestFailClosed_KnownPIIEndpointStillFullBody(t *testing.T) {
	reg := NewRegistryFailClosed()
	assert.True(t, reg.IsFullBodyPII("/orders/v0/orders/{orderId}/buyerInfo"))
}

func TestFailClosed_KnownPartialPIIEndpointNotFullBody(t *testing.T) {
	reg := NewRegistryFailClosed()
	// /orders/v0/orders has unconditional partial-PII rules. It must NOT be
	// reported as full-body even in fail-closed mode, so logging keeps the
	// non-PII fields readable.
	assert.False(t, reg.IsFullBodyPII("/orders/v0/orders"))
}

func TestFailClosed_KnownConditionalPIIEndpointNotFullBody(t *testing.T) {
	reg := NewRegistryFailClosed()
	// /orders/2026-01-01/orders has only conditional PII rules. It is in
	// the registry, so it is not "unknown" and should not be flagged as
	// full-body.
	assert.False(t, reg.IsFullBodyPII("/orders/2026-01-01/orders"))
}

func TestFailClosed_ContainsPII_UnknownEndpoint(t *testing.T) {
	reg := NewRegistryFailClosed()
	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/futureapi/2030-01-01/unknown"),
	}
	assert.True(t, reg.ContainsPII(r), "fail-closed must treat unknown SP-API paths as PII")
}

func TestFailClosed_ContainsPII_NonGETUnknownEndpoint(t *testing.T) {
	// Even in fail-closed, non-GET cannot return cached PII (no caching of
	// mutations), so ContainsPII stays false.
	reg := NewRegistryFailClosed()
	r := &http.Request{
		Method: http.MethodPost,
		URL:    mustParseURL("/futureapi/2030-01-01/unknown"),
	}
	assert.False(t, reg.ContainsPII(r))
}

func TestFailClosed_ContainsPII_KnownParameterizedNonPIIEndpoint(t *testing.T) {
	// A known parameterized SP-API endpoint that has no PII rules:
	// /catalog/2022-04-01/items/{asin} is registered (see endpoint/classify.go)
	// and lives in no PII rule map. ClassifyKnown returns ok=true, so the
	// fail-closed unknown-path branch does NOT trip. The endpoint is also
	// not in the full-body PII set or partial rules; ContainsPII therefore
	// returns false even in fail-closed mode.
	reg := NewRegistryFailClosed()
	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/catalog/2022-04-01/items/B07XYZ123"),
	}
	assert.False(t, reg.ContainsPII(r))
}

func TestFailOpen_ContainsPII_UnknownEndpoint(t *testing.T) {
	// Default mode (fail-open): unknown endpoints are NOT flagged.
	reg := NewRegistry()
	r := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("/futureapi/2030-01-01/unknown"),
	}
	assert.False(t, reg.ContainsPII(r))
}

func TestFailOpen_IsFullBodyPII_UnknownEndpoint(t *testing.T) {
	// Default mode: unknown patterns are NOT full-body.
	reg := NewRegistry()
	assert.False(t, reg.IsFullBodyPII("/futureapi/2030-01-01/unknown"))
}

func TestRegulatedInfo_HasFieldRules(t *testing.T) {
	reg := NewRegistry()
	rules := reg.RulesFor("/orders/v0/orders/{orderId}/regulatedInfo")
	assert.NotEmpty(t, rules, "expected rules for /orders/v0/orders/{orderId}/regulatedInfo")

	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	assert.Contains(t, paths, "$.payload.RegulatedInformation.Fields[*].FieldValue")
	assert.Contains(t, paths, "$.payload.BuyerInfo.BuyerEmail")
	assert.Contains(t, paths, "$.payload.ShippingAddress.AddressLine1")
}

func TestRegulatedInfo_NotFullBody(t *testing.T) {
	reg := NewRegistry()
	// regulatedInfo uses field rules (not full-body) so verification metadata
	// stays visible for operator debugging while PII fields are redacted.
	assert.False(t, reg.IsFullBodyPII("/orders/v0/orders/{orderId}/regulatedInfo"))
}

func TestSingleOrderV0_HasFieldRules(t *testing.T) {
	reg := NewRegistry()
	rules := reg.RulesFor("/orders/v0/orders/{orderId}")
	assert.NotEmpty(t, rules, "expected rules for /orders/v0/orders/{orderId}")

	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	// Single-order endpoint has the order directly under $.payload (no Orders[*] array).
	assert.Contains(t, paths, "$.payload.BuyerInfo.BuyerEmail")
	assert.Contains(t, paths, "$.payload.BuyerInfo.BuyerName")
	assert.Contains(t, paths, "$.payload.BuyerInfo.BuyerTaxInfo")
	assert.Contains(t, paths, "$.payload.ShippingAddress.AddressLine1")
	assert.Contains(t, paths, "$.payload.ShippingAddress.PostalCode")
	assert.Contains(t, paths, "$.payload.ShippingAddress.Phone")
}

func TestSingleOrderItemsV0_HasFieldRules(t *testing.T) {
	reg := NewRegistry()
	rules := reg.RulesFor("/orders/v0/orders/{orderId}/orderItems")
	assert.NotEmpty(t, rules, "expected rules for /orders/v0/orders/{orderId}/orderItems")

	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	assert.Contains(t, paths, "$.payload.OrderItems[*].BuyerInfo.BuyerCustomizedInfo")
	assert.Contains(t, paths, "$.payload.OrderItems[*].BuyerInfo.GiftMessageText")
}

func TestDocumentURLs_AreRedacted_Reports(t *testing.T) {
	reg := NewRegistry()
	rules := reg.RulesFor("/reports/2021-06-30/documents/{documentId}")
	assert.NotEmpty(t, rules)

	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	assert.Contains(t, paths, "$.url")
	assert.Contains(t, paths, "$.encryptionDetails.key")
}

func TestDocumentURLs_AreRedacted_Feeds(t *testing.T) {
	reg := NewRegistry()
	rules := reg.RulesFor("/feeds/2021-06-30/documents/{feedDocumentId}")
	assert.NotEmpty(t, rules)

	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	assert.Contains(t, paths, "$.url")
	assert.Contains(t, paths, "$.encryptionDetails.key")
}

func TestDocumentURLs_AreRedacted_DataKiosk(t *testing.T) {
	reg := NewRegistry()
	rules := reg.RulesFor("/datakiosk/2023-11-15/documents/{documentId}")
	assert.NotEmpty(t, rules)

	var paths []string
	for _, r := range rules {
		paths = append(paths, r.JSONPath)
	}
	assert.Contains(t, paths, "$.documentUrl")
}

func TestDocumentURL_RedactionRoundTrip(t *testing.T) {
	// Verifies that RedactForLogging actually replaces the URL value with
	// the redaction marker for a Reports document response.
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{"url":"https://tortuga-prod.s3.amazonaws.com/abc?X-Amz-Signature=secret","encryptionDetails":{"standard":"AES","initializationVector":"iv","key":"keymat"},"compressionAlgorithm":"GZIP"}`)
	redacted, wasPII := eng.RedactForLogging("/reports/2021-06-30/documents/{documentId}", body)

	assert.True(t, wasPII)
	assert.NotContains(t, string(redacted), "tortuga-prod.s3.amazonaws.com")
	assert.NotContains(t, string(redacted), "X-Amz-Signature=secret")
	assert.NotContains(t, string(redacted), "keymat")
	// Non-PII fields remain visible
	assert.Contains(t, string(redacted), "GZIP")
}

func TestRegistry_QueryParamsExtra_Default(t *testing.T) {
	reg := NewRegistry()
	assert.Empty(t, reg.QueryParamsExtra())
}

func TestRegistry_QueryParamsExtra_Custom(t *testing.T) {
	reg := NewRegistryWithExtras([]string{"FooParam", "BarParam"})
	got := reg.QueryParamsExtra()
	// Keys must be lower-cased for case-insensitive matching.
	assert.True(t, got["fooparam"])
	assert.True(t, got["barparam"])
	assert.False(t, got["FooParam"])
}

func TestRegistry_SetFailClosed(t *testing.T) {
	reg := NewRegistryWithExtras([]string{"foo"})
	assert.False(t, reg.FailClosed())
	reg.SetFailClosed(true)
	assert.True(t, reg.FailClosed())
}
