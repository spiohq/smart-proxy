package rdt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchPIIOperation_RestrictedEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		path          string
		wantMatch     bool
		wantGeneric   string   // the generic path form for RDT minting
		wantDataElems []string // expected dataElements (nil if none)
		wantCacheable bool
	}{
		// ── Orders v0: getOrders (list) ────────────────────────────────────
		{
			name:          "getOrders list",
			method:        "GET",
			path:          "/orders/v0/orders",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders",
			wantDataElems: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
			wantCacheable: true,
		},
		{
			name:          "getOrders list with query params",
			method:        "GET",
			path:          "/orders/v0/orders?MarketplaceIds=ATVPDKIKX0DER&CreatedAfter=2024-01-01",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders",
			wantDataElems: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
			wantCacheable: true,
		},
		// ── Orders v0: getOrder ────────────────────────────────────────────
		{
			name:          "getOrder with specific order ID",
			method:        "GET",
			path:          "/orders/v0/orders/123-4567890-1234567",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders/{orderId}",
			wantDataElems: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
			wantCacheable: true,
		},
		// ── Orders v0: getOrderItems ──────────────────────────────────────
		{
			name:          "getOrderItems",
			method:        "GET",
			path:          "/orders/v0/orders/123-4567890-1234567/orderItems",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders/{orderId}/orderItems",
			wantDataElems: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
			wantCacheable: true,
		},
		// ── Orders v0: getOrderAddress ────────────────────────────────────
		{
			name:          "getOrderAddress",
			method:        "GET",
			path:          "/orders/v0/orders/123-4567890-1234567/address",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders/{orderId}/address",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Orders v0: getOrderBuyerInfo ──────────────────────────────────
		{
			name:          "getOrderBuyerInfo",
			method:        "GET",
			path:          "/orders/v0/orders/123-4567890-1234567/buyerInfo",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders/{orderId}/buyerInfo",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Orders v0: getOrderItemsBuyerInfo ─────────────────────────────
		{
			name:          "getOrderItemsBuyerInfo",
			method:        "GET",
			path:          "/orders/v0/orders/123-4567890-1234567/orderItems/buyerInfo",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders/{orderId}/orderItems/buyerInfo",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Orders v0: getOrderRegulatedInfo ──────────────────────────────
		{
			name:          "getOrderRegulatedInfo",
			method:        "GET",
			path:          "/orders/v0/orders/123-4567890-1234567/regulatedInfo",
			wantMatch:     true,
			wantGeneric:   "/orders/v0/orders/{orderId}/regulatedInfo",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── MFN: getShipment ──────────────────────────────────────────────
		{
			name:          "MFN getShipment",
			method:        "GET",
			path:          "/mfn/v0/shipments/abcd1234",
			wantMatch:     true,
			wantGeneric:   "/mfn/v0/shipments/{shipmentId}",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── MFN: cancelShipment ───────────────────────────────────────────
		{
			name:          "MFN cancelShipment",
			method:        "PUT",
			path:          "/mfn/v0/shipments/abcd1234/cancel",
			wantMatch:     true,
			wantGeneric:   "/mfn/v0/shipments/{shipmentId}/cancel",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── MFN: createShipment ───────────────────────────────────────────
		{
			name:          "MFN createShipment",
			method:        "POST",
			path:          "/mfn/v0/shipments",
			wantMatch:     true,
			wantGeneric:   "/mfn/v0/shipments",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Shipping v1: getShipment ──────────────────────────────────────
		{
			name:          "Shipping v1 getShipment",
			method:        "GET",
			path:          "/shipping/v1/shipments/ship-1234",
			wantMatch:     true,
			wantGeneric:   "/shipping/v1/shipments/{shipmentId}",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Shipment Invoicing (Brazil) ───────────────────────────────────
		{
			name:          "Shipment Invoicing Brazil",
			method:        "GET",
			path:          "/fba/outbound/brazil/v0/shipments/FBA1234",
			wantMatch:     true,
			wantGeneric:   "/fba/outbound/brazil/v0/shipments/{shipmentId}",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Easy Ship bulk ────────────────────────────────────────────────
		{
			name:          "Easy Ship bulk",
			method:        "POST",
			path:          "/easyShip/2022-03-23/packages/bulk",
			wantMatch:     true,
			wantGeneric:   "/easyShip/2022-03-23/packages/bulk",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Direct Fulfillment Orders: getOrders ──────────────────────────
		{
			name:          "DF Orders getOrders",
			method:        "GET",
			path:          "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Direct Fulfillment Orders: getOrder ───────────────────────────
		{
			name:          "DF Orders getOrder",
			method:        "GET",
			path:          "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/PO-12345",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/{purchaseOrderNumber}",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Direct Fulfillment Shipping: shippingLabels ───────────────────
		{
			name:          "DF Shipping shippingLabels list",
			method:        "GET",
			path:          "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels",
			wantDataElems: nil,
			wantCacheable: true,
		},
		{
			name:          "DF Shipping shippingLabels single",
			method:        "GET",
			path:          "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/PO-12345",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/{purchaseOrderNumber}",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Direct Fulfillment Shipping: packingSlips ─────────────────────
		{
			name:          "DF Shipping packingSlips list",
			method:        "GET",
			path:          "/vendor/directFulfillment/shipping/2021-12-28/packingSlips",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/shipping/2021-12-28/packingSlips",
			wantDataElems: nil,
			wantCacheable: true,
		},
		{
			name:          "DF Shipping packingSlips single",
			method:        "GET",
			path:          "/vendor/directFulfillment/shipping/2021-12-28/packingSlips/PO-12345",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/shipping/2021-12-28/packingSlips/{purchaseOrderNumber}",
			wantDataElems: nil,
			wantCacheable: true,
		},
		// ── Direct Fulfillment Shipping: customerInvoices ─────────────────
		{
			name:          "DF Shipping customerInvoices list",
			method:        "GET",
			path:          "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices",
			wantDataElems: nil,
			wantCacheable: true,
		},
		{
			name:          "DF Shipping customerInvoices single",
			method:        "GET",
			path:          "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/PO-12345",
			wantMatch:     true,
			wantGeneric:   "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/{purchaseOrderNumber}",
			wantDataElems: nil,
			wantCacheable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, ok := MatchPIIOperation(tt.method, tt.path)
			require.Equal(t, tt.wantMatch, ok, "match mismatch for %s %s", tt.method, tt.path)
			if !ok {
				return
			}
			assert.Equal(t, tt.wantGeneric, op.GenericPath)
			assert.Equal(t, tt.wantDataElems, op.DataElements)
			assert.Equal(t, tt.wantCacheable, op.Cacheable)
		})
	}
}

func TestMatchPIIOperation_NonRestrictedEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"catalog items", "GET", "/catalog/2022-04-01/items/B07XYZ1234"},
		{"listings", "GET", "/listings/2021-08-01/items/ATVPDKIKX0DER/SKU123"},
		{"product pricing", "GET", "/products/pricing/v0/items/B07XYZ1234/offers"},
		{"feeds", "POST", "/feeds/2021-06-30/feeds"},
		{"notifications", "GET", "/notifications/v1/subscriptions/ANY_OFFER_CHANGED"},
		{"finances", "GET", "/finances/v0/financialEventGroups/abc123/financialEvents"},
		{"report documents (handled by ReportTracker)", "GET", "/reports/2021-06-30/documents/amzn1.spdoc.1.4.na.abc123"},
		{"tokens endpoint itself", "POST", "/tokens/2021-03-01/restrictedDataToken"},
		{"random unknown path", "GET", "/some/unknown/path"},
		{"orders v2026 (no RDT needed)", "GET", "/orders/2026-01-01/orders/123-456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := MatchPIIOperation(tt.method, tt.path)
			assert.False(t, ok, "should NOT match as PII operation: %s %s", tt.method, tt.path)
		})
	}
}

func TestMatchPIIOperation_TrailingSlash(t *testing.T) {
	op, ok := MatchPIIOperation("GET", "/orders/v0/orders/123-456/")
	require.True(t, ok)
	assert.Equal(t, "/orders/v0/orders/{orderId}", op.GenericPath)
}

func TestMatchPIIOperation_QueryStringStripped(t *testing.T) {
	op, ok := MatchPIIOperation("GET", "/mfn/v0/shipments/abc123?foo=bar&baz=1")
	require.True(t, ok)
	assert.Equal(t, "/mfn/v0/shipments/{shipmentId}", op.GenericPath)
}

func TestPIIOperation_ToRestrictedResource(t *testing.T) {
	// Orders v0 with dataElements
	op := PIIOperation{
		GenericPath:  "/orders/v0/orders/{orderId}",
		Method:       "GET",
		DataElements: []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"},
		Cacheable:    true,
	}
	rr := op.ToRestrictedResource()
	assert.Equal(t, "GET", rr.Method)
	assert.Equal(t, "/orders/v0/orders/{orderId}", rr.Path)
	assert.Equal(t, []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"}, rr.DataElements)

	// MFN without dataElements
	op2 := PIIOperation{
		GenericPath:  "/mfn/v0/shipments/{shipmentId}",
		Method:       "GET",
		DataElements: nil,
		Cacheable:    true,
	}
	rr2 := op2.ToRestrictedResource()
	assert.Equal(t, "GET", rr2.Method)
	assert.Equal(t, "/mfn/v0/shipments/{shipmentId}", rr2.Path)
	assert.Nil(t, rr2.DataElements)
}
