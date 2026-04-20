package ratelimit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultBucketParams_Count(t *testing.T) {
	assert.GreaterOrEqual(t, len(DefaultBucketParams), 140,
		"DefaultBucketParams should have at least 140 entries to prevent accidental deletions")
}

func TestDefaultBucketParams_AllValid(t *testing.T) {
	for key, params := range DefaultBucketParams {
		t.Run(key, func(t *testing.T) {
			assert.Greater(t, params.Rate, 0.0, "Rate must be > 0")
			assert.Greater(t, params.Burst, 0.0, "Burst must be > 0")
		})
	}
}

func TestDefaultBucketParams_ParameterizedKeysHaveClassifyPatterns(t *testing.T) {
	classifyPatterns := make(map[string]bool)

	for key := range DefaultBucketParams {
		if !strings.Contains(key, "{") {
			continue
		}
		synthetic := replaceParams(key)
		classified := ClassifyEndpoint(synthetic)
		classifyPatterns[classified] = true
	}

	for key := range DefaultBucketParams {
		if !strings.Contains(key, "{") {
			continue
		}
		t.Run(key, func(t *testing.T) {
			assert.True(t, classifyPatterns[key],
				"parameterized key %q in DefaultBucketParams has no matching classify pattern (classify returned %q for synthetic input)",
				key, ClassifyEndpoint(replaceParams(key)))
		})
	}
}

func replaceParams(pattern string) string {
	result := pattern
	for {
		start := strings.Index(result, "{")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		result = result[:start] + "TESTVALUE" + result[start+end+1:]
	}
	return result
}

func TestDefaultBucketParams_SpotChecks(t *testing.T) {
	tests := []struct {
		name          string
		method        string // defaults to "GET" if empty
		endpoint      string
		expectedRate  float64
		expectedBurst float64
	}{
		// ── Orders v0 ──────────────────────────────────────────────────
		{name: "orders v0 getOrders", endpoint: "/orders/v0/orders", expectedRate: 0.0167, expectedBurst: 20},
		{name: "orders v0 getOrder", endpoint: "/orders/v0/orders/{orderId}", expectedRate: 0.5, expectedBurst: 30},
		{name: "orders v0 confirmShipment", method: "POST", endpoint: "/orders/v0/orders/{orderId}/shipmentConfirmation", expectedRate: 2.0, expectedBurst: 10},
		{name: "orders v0 updateShipmentStatus", method: "POST", endpoint: "/orders/v0/orders/{orderId}/shipment", expectedRate: 5.0, expectedBurst: 15},

		// ── Orders v2026-01-01 ─────────────────────────────────────────
		{name: "orders v2026 searchOrders", endpoint: "/orders/2026-01-01/orders", expectedRate: 0.0056, expectedBurst: 20},
		{name: "orders v2026 getOrder", endpoint: "/orders/2026-01-01/orders/{orderId}", expectedRate: 0.5, expectedBurst: 30},

		// ── Catalog Items v2022-04-01 ──────────────────────────────────
		{name: "catalog searchCatalogItems", endpoint: "/catalog/2022-04-01/items", expectedRate: 2.0, expectedBurst: 2},
		{name: "catalog getCatalogItem", endpoint: "/catalog/2022-04-01/items/{asin}", expectedRate: 2.0, expectedBurst: 2},

		// ── Feeds v2021-06-30 ──────────────────────────────────────────
		{name: "feeds getFeeds", endpoint: "/feeds/2021-06-30/feeds", expectedRate: 0.0222, expectedBurst: 10},
		{name: "feeds createFeed", method: "POST", endpoint: "/feeds/2021-06-30/feeds", expectedRate: 0.0083, expectedBurst: 15},
		{name: "feeds getFeed", endpoint: "/feeds/2021-06-30/feeds/{feedId}", expectedRate: 2.0, expectedBurst: 15},
		{name: "feeds createFeedDocument", method: "POST", endpoint: "/feeds/2021-06-30/documents", expectedRate: 0.5, expectedBurst: 15},
		{name: "feeds getFeedDocument", endpoint: "/feeds/2021-06-30/documents/{feedDocumentId}", expectedRate: 0.0222, expectedBurst: 10},

		// ── Reports v2021-06-30 ────────────────────────────────────────
		{name: "reports getReports", endpoint: "/reports/2021-06-30/reports", expectedRate: 0.0222, expectedBurst: 10},
		{name: "reports createReport", method: "POST", endpoint: "/reports/2021-06-30/reports", expectedRate: 0.0167, expectedBurst: 15},
		{name: "reports getReport", endpoint: "/reports/2021-06-30/reports/{reportId}", expectedRate: 2.0, expectedBurst: 15},
		{name: "reports getReportSchedules", endpoint: "/reports/2021-06-30/schedules", expectedRate: 0.0222, expectedBurst: 10},
		{name: "reports getReportDocument", endpoint: "/reports/2021-06-30/documents/{documentId}", expectedRate: 0.0167, expectedBurst: 15},

		// ── Data Kiosk v2023-11-15 ─────────────────────────────────────
		{name: "datakiosk getQueries", endpoint: "/datakiosk/2023-11-15/queries", expectedRate: 0.0222, expectedBurst: 10},
		{name: "datakiosk createQuery", method: "POST", endpoint: "/datakiosk/2023-11-15/queries", expectedRate: 0.0167, expectedBurst: 15},
		{name: "datakiosk getQuery", endpoint: "/datakiosk/2023-11-15/queries/{queryId}", expectedRate: 2.0, expectedBurst: 15},
		{name: "datakiosk getDocument", endpoint: "/datakiosk/2023-11-15/documents/{documentId}", expectedRate: 0.0167, expectedBurst: 15},

		// ── Listings Items v2021-08-01 ─────────────────────────────────
		{name: "listings getListingsItem", endpoint: "/listings/2021-08-01/items/{sellerId}/{sku}", expectedRate: 5.0, expectedBurst: 5},
		{name: "listings searchListingsItems", endpoint: "/listings/2021-08-01/items/{sellerId}", expectedRate: 5.0, expectedBurst: 5},
		{name: "listings putListingsItem", method: "PUT", endpoint: "/listings/2021-08-01/items/{sellerId}/{sku}", expectedRate: 5.0, expectedBurst: 5},
		{name: "listings patchListingsItem", method: "PATCH", endpoint: "/listings/2021-08-01/items/{sellerId}/{sku}", expectedRate: 5.0, expectedBurst: 5},
		{name: "listings deleteListingsItem", method: "DELETE", endpoint: "/listings/2021-08-01/items/{sellerId}/{sku}", expectedRate: 5.0, expectedBurst: 5},

		// ── Listings Restrictions v2021-08-01 ──────────────────────────
		{name: "listings restrictions", endpoint: "/listings/2021-08-01/restrictions", expectedRate: 5.0, expectedBurst: 10},

		// ── Product Pricing v0 ─────────────────────────────────────────
		{name: "pricing getCompetitivePricing", endpoint: "/products/pricing/v0/competitivePrice", expectedRate: 0.5, expectedBurst: 1},
		{name: "pricing getPricing", endpoint: "/products/pricing/v0/price", expectedRate: 0.5, expectedBurst: 1},
		{name: "pricing getItemOffers", endpoint: "/products/pricing/v0/items/{asin}/offers", expectedRate: 0.5, expectedBurst: 1},
		{name: "pricing getListingOffers", endpoint: "/products/pricing/v0/listings/{sku}/offers", expectedRate: 0.5, expectedBurst: 1},
		{name: "pricing getItemOffersBatch", method: "POST", endpoint: "/batches/products/pricing/v0/itemOffers", expectedRate: 0.5, expectedBurst: 1},

		// ── Product Pricing v2022-05-01 ────────────────────────────────
		{name: "pricing v2 featuredOfferExpectedPrice", method: "POST", endpoint: "/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice", expectedRate: 0.033, expectedBurst: 1},
		{name: "pricing v2 competitiveSummary", method: "POST", endpoint: "/products/pricing/2022-05-01/offer/competitiveSummary", expectedRate: 0.033, expectedBurst: 1},

		// ── Product Fees v0 ────────────────────────────────────────────
		{name: "fees estimate by sku", method: "POST", endpoint: "/products/fees/v0/listings/{sku}/feesEstimate", expectedRate: 1.0, expectedBurst: 2},
		{name: "fees estimate by asin", method: "POST", endpoint: "/products/fees/v0/items/{asin}/feesEstimate", expectedRate: 1.0, expectedBurst: 2},
		{name: "fees estimate batch", method: "POST", endpoint: "/products/fees/v0/feesEstimate", expectedRate: 0.5, expectedBurst: 1},

		// ── Product Type Definitions v2020-09-01 ───────────────────────
		{name: "product types list", endpoint: "/definitions/2020-09-01/productTypes", expectedRate: 5.0, expectedBurst: 10},
		{name: "product types by type", endpoint: "/definitions/2020-09-01/productTypes/{productType}", expectedRate: 5.0, expectedBurst: 10},

		// ── FBA Inventory v1 ───────────────────────────────────────────
		{name: "fba inventory summaries", endpoint: "/fba/inventory/v1/summaries", expectedRate: 2.0, expectedBurst: 2},
		{name: "fba inventory items", endpoint: "/fba/inventory/v1/items", expectedRate: 2.0, expectedBurst: 3},

		// ── FBA Inbound Eligibility v1 ─────────────────────────────────
		{name: "fba inbound eligibility", endpoint: "/fba/inbound/v1/eligibility/itemPreview", expectedRate: 1.0, expectedBurst: 1},

		// ── Fulfillment Inbound v2024-03-20 ────────────────────────────
		{name: "inbound plans list", endpoint: "/fba/inbound/v2024-03-20/inboundPlans", expectedRate: 2.0, expectedBurst: 6},
		{name: "inbound plan packing options", endpoint: "/fba/inbound/v2024-03-20/inboundPlans/{planId}/packingOptions", expectedRate: 2.0, expectedBurst: 2},

		// ── Fulfillment Inbound v0 (Legacy) ────────────────────────────
		{name: "inbound v0 shipments", endpoint: "/fba/inbound/v0/shipments", expectedRate: 2.0, expectedBurst: 30},
		{name: "inbound v0 shipment labels", endpoint: "/fba/inbound/v0/shipments/{shipmentId}/labels", expectedRate: 5.0, expectedBurst: 30},

		// ── Fulfillment Outbound v2020-07-01 ───────────────────────────
		{name: "fulfillment orders list", endpoint: "/fba/outbound/2020-07-01/fulfillmentOrders", expectedRate: 2.0, expectedBurst: 30},
		{name: "delivery offers", method: "POST", endpoint: "/fba/outbound/2020-07-01/deliveryOffers", expectedRate: 10.0, expectedBurst: 30},

		// ── Shipping v1 ────────────────────────────────────────────────
		{name: "shipping v1 shipments", endpoint: "/shipping/v1/shipments", expectedRate: 5.0, expectedBurst: 15},
		{name: "shipping v1 tracking", endpoint: "/shipping/v1/tracking/{trackingId}", expectedRate: 1.0, expectedBurst: 1},

		// ── Shipping v2 ────────────────────────────────────────────────
		{name: "shipping v2 oneClickShipment", endpoint: "/shipping/v2/oneClickShipment", expectedRate: 5.0, expectedBurst: 15},
		{name: "shipping v2 collectionForms", endpoint: "/shipping/v2/collectionForms", expectedRate: 5.0, expectedBurst: 15},

		// ── Merchant Fulfillment v0 ────────────────────────────────────
		{name: "mfn eligible shipping", method: "POST", endpoint: "/mfn/v0/eligibleShippingServices", expectedRate: 6.0, expectedBurst: 12},

		// ── Easy Ship v2022-03-23 ──────────────────────────────────────
		{name: "easy ship timeSlot", endpoint: "/easyShip/2022-03-23/timeSlot", expectedRate: 1.0, expectedBurst: 1},
		{name: "easy ship package", endpoint: "/easyShip/2022-03-23/package", expectedRate: 1.0, expectedBurst: 1},

		// ── Notifications v1 ───────────────────────────────────────────
		{name: "notifications subscriptions by type", endpoint: "/notifications/v1/subscriptions/{type}", expectedRate: 1.0, expectedBurst: 5},
		{name: "notifications destinations", endpoint: "/notifications/v1/destinations", expectedRate: 1.0, expectedBurst: 5},

		// ── Tokens v2021-03-01 ─────────────────────────────────────────
		{name: "restricted data token", method: "POST", endpoint: "/tokens/2021-03-01/restrictedDataToken", expectedRate: 1.0, expectedBurst: 10},

		// ── Sellers v1 ─────────────────────────────────────────────────
		{name: "sellers marketplace participations", endpoint: "/sellers/v1/marketplaceParticipations", expectedRate: 0.016, expectedBurst: 15},

		// ── Sales v1 ───────────────────────────────────────────────────
		{name: "sales order metrics", endpoint: "/sales/v1/orderMetrics", expectedRate: 0.5, expectedBurst: 15},

		// ── Finances v0 ────────────────────────────────────────────────
		{name: "financial events", endpoint: "/finances/v0/financialEvents", expectedRate: 0.5, expectedBurst: 30},

		// ── Finances v2024-06-19 ───────────────────────────────────────
		{name: "finances v2024 transactions", endpoint: "/finances/2024-06-19/transactions", expectedRate: 0.5, expectedBurst: 10},

		// ── Messaging v1 ───────────────────────────────────────────────
		{name: "messaging messages", endpoint: "/messaging/v1/orders/{orderId}/messages", expectedRate: 1.0, expectedBurst: 5},

		// ── Solicitations v1 ───────────────────────────────────────────
		{name: "solicitations list", endpoint: "/solicitations/v1/orders/{orderId}/solicitations", expectedRate: 1.0, expectedBurst: 5},

		// ── Uploads v2020-11-01 ────────────────────────────────────────
		{name: "upload destinations", method: "POST", endpoint: "/uploads/2020-11-01/uploadDestinations/{resource}", expectedRate: 0.1, expectedBurst: 5},

		// ── Application Management v2023-11-30 ─────────────────────────
		{name: "client secret", method: "POST", endpoint: "/applications/2023-11-30/clientSecret", expectedRate: 0.0167, expectedBurst: 1},

		// ── A+ Content v2020-11-01 ─────────────────────────────────────
		{name: "aplus content documents", endpoint: "/aplus/2020-11-01/contentDocuments", expectedRate: 10.0, expectedBurst: 10},
		{name: "aplus content by key", endpoint: "/aplus/2020-11-01/contentDocuments/{contentReferenceKey}", expectedRate: 10.0, expectedBurst: 10},

		// ── Replenishment v2022-11-07 ──────────────────────────────────
		{name: "replenishment selling partners metrics", method: "POST", endpoint: "/replenishment/2022-11-07/sellingPartners/metrics/search", expectedRate: 1.0, expectedBurst: 1},

		// ── AWD v2024-05-09 ────────────────────────────────────────────
		{name: "awd inbound shipments list", endpoint: "/awd/2024-05-09/inboundShipments", expectedRate: 1.0, expectedBurst: 1},
		{name: "awd inbound shipment by id", endpoint: "/awd/2024-05-09/inboundShipments/{shipmentId}", expectedRate: 2.0, expectedBurst: 6},
		{name: "awd inventory", endpoint: "/awd/2024-05-09/inventory", expectedRate: 2.0, expectedBurst: 2},

		// ── Supply Sources v2020-07-01 ─────────────────────────────────
		{name: "supply sources list", endpoint: "/supplySources/2020-07-01/supplySources", expectedRate: 1.0, expectedBurst: 10},
		{name: "supply source by id", endpoint: "/supplySources/2020-07-01/supplySources/{supplySourceId}", expectedRate: 1.0, expectedBurst: 10},

		// ── Vendor APIs (all 10/10) ────────────────────────────────────
		{name: "vendor df purchase orders", endpoint: "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor df shipping labels", endpoint: "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor df inventory updates", endpoint: "/vendor/directFulfillment/inventory/2021-12-28/inventoryUpdates", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor df invoices", endpoint: "/vendor/directFulfillment/payments/2021-12-28/invoices", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor df transaction", endpoint: "/vendor/directFulfillment/transactions/2021-12-28/transactions/{transactionId}", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor purchase orders", endpoint: "/vendor/orders/v1/purchaseOrders", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor shipment confirmations", endpoint: "/vendor/shipments/v1/shipmentConfirmations", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor invoices", endpoint: "/vendor/invoices/v1/invoices", expectedRate: 10.0, expectedBurst: 10},
		{name: "vendor transaction status", endpoint: "/vendor/transactionStatus/v1/transactions/{transactionId}", expectedRate: 10.0, expectedBurst: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = "GET"
			}
			params, ok := LookupDefaults(method, tt.endpoint)
			require.True(t, ok, "endpoint %q (method %s) not found", tt.endpoint, method)
			assert.InDelta(t, tt.expectedRate, params.Rate, 0.0001, "Rate mismatch for %s %q", method, tt.endpoint)
			assert.InDelta(t, tt.expectedBurst, params.Burst, 0.1, "Burst mismatch for %s %q", method, tt.endpoint)
		})
	}
}

func TestMethodBucketOverrides_AllValid(t *testing.T) {
	for key, params := range MethodBucketOverrides {
		t.Run(key, func(t *testing.T) {
			assert.Greater(t, params.Rate, 0.0, "Rate must be > 0")
			assert.Greater(t, params.Burst, 0.0, "Burst must be > 0")
			assert.Contains(t, key, ":", "key must be METHOD:endpoint format")
		})
	}
}

func TestAppBucketParams_AllValid(t *testing.T) {
	for key, params := range AppBucketParams {
		t.Run(key, func(t *testing.T) {
			assert.Greater(t, params.Rate, 0.0, "Rate must be > 0")
			assert.Greater(t, params.Burst, 0.0, "Burst must be > 0")
			assert.Contains(t, key, ":", "key must be METHOD:endpoint format")
		})
	}
}

func TestClassifyEndpoint_ExactMatch(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/orders/v0/orders", "/orders/v0/orders"},
		{"/catalog/2022-04-01/items", "/catalog/2022-04-01/items"},
		{"/finances/v0/financialEvents", "/finances/v0/financialEvents"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, ClassifyEndpoint(tt.path))
		})
	}
}

func TestClassifyEndpoint_ParameterizedPaths(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/orders/v0/orders/123-456-789", "/orders/v0/orders/{orderId}"},
		{"/orders/v0/orders/123-456-789/orderItems", "/orders/v0/orders/{orderId}/orderItems"},
		{"/catalog/2022-04-01/items/B08N5WRWNW", "/catalog/2022-04-01/items/{asin}"},
		{"/feeds/2021-06-30/feeds/FEED-12345", "/feeds/2021-06-30/feeds/{feedId}"},
		{"/reports/2021-06-30/reports/RPT-67890", "/reports/2021-06-30/reports/{reportId}"},
		{"/listings/2021-08-01/items/ATVPDKIKX0DER/MY-SKU-123", "/listings/2021-08-01/items/{sellerId}/{sku}"},
		{"/notifications/v1/subscriptions/BRANDED_ITEM_CONTENT_CHANGE", "/notifications/v1/subscriptions/{type}"},
		{"/products/pricing/v0/items/B08N5WRWNW/offers", "/products/pricing/v0/items/{asin}/offers"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, ClassifyEndpoint(tt.path))
		})
	}
}

func TestClassifyEndpoint_UnknownEndpoint_ReturnsAsIs(t *testing.T) {
	result := ClassifyEndpoint("/new-api/2027-01-01/widgets/123")
	assert.Equal(t, "/new-api/2027-01-01/widgets/123", result)
}

func TestClassifyEndpoint_WithQueryString_StripsQuery(t *testing.T) {
	result := ClassifyEndpoint("/orders/v0/orders?MarketplaceIds=A1PA6795UKMFR9")
	assert.Equal(t, "/orders/v0/orders", result)
}

func TestDefaultBucketParams_KnownEndpoints(t *testing.T) {
	params, ok := LookupDefaults("GET", "/orders/v0/orders")
	assert.True(t, ok)
	assert.InDelta(t, 0.0167, params.Rate, 0.0001)
	assert.InDelta(t, 20.0, params.Burst, 0.1)
}

func TestDefaultBucketParams_UnknownEndpoint(t *testing.T) {
	_, ok := LookupDefaults("GET", "/unknown/v1/endpoint")
	assert.False(t, ok)
}

func TestLookupDefaults_MethodOverride_FeedsCreateFeed(t *testing.T) {
	// GET /feeds should return getFeeds rate (0.0222)
	getParams, ok := LookupDefaults("GET", "/feeds/2021-06-30/feeds")
	require.True(t, ok)
	assert.InDelta(t, 0.0222, getParams.Rate, 0.0001)

	// POST /feeds should return createFeed rate (0.0083) from override
	postParams, ok := LookupDefaults("POST", "/feeds/2021-06-30/feeds")
	require.True(t, ok)
	assert.InDelta(t, 0.0083, postParams.Rate, 0.0001)
}
