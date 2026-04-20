package cache_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/stretchr/testify/assert"
)

func TestTierClassifier_NonGetIsNever(t *testing.T) {
	tc := cache.NewTierClassifier(nil)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		tier := tc.Classify(method, "/orders/v0/orders", nil)
		assert.Equal(t, cache.CacheTierNever, tier.Tier, "method %s should be Never", method)
	}
}

func TestTierClassifier_KnownEndpoints(t *testing.T) {
	tc := cache.NewTierClassifier(nil)
	tests := []struct {
		path string
		tier cache.CacheTier
	}{
		{"/catalog/2022-04-01/items", cache.CacheTierAggressive},
		{"/catalog/2022-04-01/items/B08N5WRWNW", cache.CacheTierAggressive},
		{"/definitions/2020-09-01/productTypes", cache.CacheTierAggressive},
		{"/products/pricing/v0/competitivePrice", cache.CacheTierShort},
		{"/fba/inventory/v1/items/inventory", cache.CacheTierModerate},
		{"/reports/2021-06-30/reports", cache.CacheTierModerate},
		{"/orders/v0/orders", cache.CacheTierShort},
		{"/listings/2021-08-01/items/SELLER/SKU", cache.CacheTierModerate},
		// Feeds GET endpoints ARE cacheable (Research §19)
		{"/feeds/2021-06-30/feeds", cache.CacheTierModerate},
		// Notifications GET endpoints ARE cacheable (Research §21)
		{"/notifications/v1/subscriptions/ANY_OFFER_CHANGED", cache.CacheTierModerate},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			tier := tc.Classify("GET", tt.path, nil)
			assert.Equal(t, tt.tier, tier.Tier)
		})
	}
}

func TestTierClassifier_UnknownEndpoint(t *testing.T) {
	tc := cache.NewTierClassifier(nil)
	tier := tc.Classify("GET", "/new-api/2027-01-01/widgets", nil)
	assert.Equal(t, cache.CacheTierShort, tier.Tier, "unknown endpoint defaults to Short")
	assert.Equal(t, 60*time.Second, tier.DefaultTTL)
}

func TestTierClassifier_PIIChecker(t *testing.T) {
	checker := func(r *http.Request) bool {
		return r.URL.Query().Get("dataElements") == "buyerInfo"
	}
	tc := cache.NewTierClassifier(checker)

	// Without PII query param  -  cacheable
	reqNoPII, _ := http.NewRequest("GET", "/orders/v0/orders", nil)
	tier := tc.Classify("GET", "/orders/v0/orders", reqNoPII)
	assert.Equal(t, cache.CacheTierShort, tier.Tier)

	// With PII query param  -  not cacheable
	reqPII, _ := http.NewRequest("GET", "/orders/v0/orders?dataElements=buyerInfo", nil)
	tier = tc.Classify("GET", "/orders/v0/orders", reqPII)
	assert.Equal(t, cache.CacheTierNever, tier.Tier)
	assert.Equal(t, "PII_EXCLUDED", tier.Reason)
}

func TestTierClassifier_DefaultTTLs(t *testing.T) {
	tc := cache.NewTierClassifier(nil)

	tier := tc.Classify("GET", "/catalog/2022-04-01/items", nil)
	assert.Equal(t, 12*time.Hour, tier.DefaultTTL)

	tier = tc.Classify("GET", "/reports/2021-06-30/reports", nil)
	assert.Equal(t, 10*time.Minute, tier.DefaultTTL)

	tier = tc.Classify("GET", "/orders/v0/orders", nil)
	assert.Equal(t, 60*time.Second, tier.DefaultTTL)
}

// TestTierClassifier_ResearchAligned validates tier classifications against
// the comprehensive SP-API Caching Research (CACHING_RESEARCH.md).
// Each test case references the research section that justifies the classification.
func TestTierClassifier_ResearchAligned(t *testing.T) {
	tc := cache.NewTierClassifier(nil)
	tests := []struct {
		name string
		path string
		tier cache.CacheTier
		ttl  time.Duration
	}{
		// ── Aggressive tier (stable reference data) ──────────────────
		// Research §2: Product Type Definitions  -  schemas change extremely rarely
		{"definitions", "/definitions/2020-09-01/productTypes", cache.CacheTierAggressive, 24 * time.Hour},
		{"definitions single", "/definitions/2020-09-01/productTypes/LUGGAGE", cache.CacheTierAggressive, 24 * time.Hour},
		// Research §3: Catalog Items  -  stable catalog data
		{"catalog items", "/catalog/2022-04-01/items", cache.CacheTierAggressive, 12 * time.Hour},
		{"catalog item single", "/catalog/2022-04-01/items/B08N5WRWNW", cache.CacheTierAggressive, 12 * time.Hour},
		// Research §22: Sellers  -  quasi-static, 0.016/s rate limit
		{"sellers participations", "/sellers/v1/marketplaceParticipations", cache.CacheTierAggressive, 12 * time.Hour},
		{"sellers account", "/sellers/v1/account", cache.CacheTierAggressive, 12 * time.Hour},

		// ── Moderate tier ────────────────────────────────────────────
		// Research §1: Listings
		{"listings", "/listings/2021-08-01/items/SELLER/SKU", cache.CacheTierModerate, 15 * time.Minute},
		// Research §4: A+ Content
		{"aplus content", "/aplus/2020-11-01/contentDocuments", cache.CacheTierModerate, 30 * time.Minute},
		{"aplus content single", "/aplus/2020-11-01/contentDocuments/REF123", cache.CacheTierModerate, 30 * time.Minute},
		// Research §8: Sales
		{"sales metrics", "/sales/v1/orderMetrics", cache.CacheTierModerate, 15 * time.Minute},
		// Research §9: FBA Inventory
		{"fba inventory", "/fba/inventory/v1/items/inventory", cache.CacheTierModerate, 5 * time.Minute},
		// Research §10: FBA Inbound Eligibility
		{"fba inbound eligibility", "/fba/inbound/eligibility/v1/eligibility", cache.CacheTierModerate, 12 * time.Hour},
		// Research §11: FBA Inbound
		{"fba inbound plans", "/fba/inbound/v2024-03-20/inboundPlans", cache.CacheTierModerate, 10 * time.Minute},
		// Research §13: Fulfillment Outbound
		{"fba outbound", "/fba/outbound/2020-07-01/fulfillmentOrders", cache.CacheTierModerate, 10 * time.Minute},
		{"fba outbound features", "/fba/outbound/2020-07-01/features", cache.CacheTierModerate, 10 * time.Minute},
		// Research §14: Merchant Fulfillment
		{"mfn shipments", "/mfn/v0/shipments/SHIP123", cache.CacheTierModerate, 15 * time.Minute},
		// Research §16: Supply Sources
		{"supply sources", "/supplySources/2020-07-01/supplySources", cache.CacheTierModerate, 1 * time.Hour},
		// Research §18: Reports
		{"reports list", "/reports/2021-06-30/reports", cache.CacheTierModerate, 10 * time.Minute},
		{"reports schedules", "/reports/2021-06-30/schedules", cache.CacheTierModerate, 10 * time.Minute},
		// Research §19: Feeds  -  GET endpoints are cacheable
		{"feeds list", "/feeds/2021-06-30/feeds", cache.CacheTierModerate, 2 * time.Minute},
		{"feed single", "/feeds/2021-06-30/feeds/FEED123", cache.CacheTierModerate, 2 * time.Minute},
		// Research §20: Finances
		{"finances v0", "/finances/v0/financialEventGroups", cache.CacheTierModerate, 15 * time.Minute},
		{"finances v2024", "/finances/2024-06-19/transactions", cache.CacheTierModerate, 15 * time.Minute},
		// Research §21: Notifications  -  GET config endpoints are cacheable
		{"notifications subscriptions", "/notifications/v1/subscriptions/ANY_OFFER_CHANGED", cache.CacheTierModerate, 30 * time.Minute},
		{"notifications destinations", "/notifications/v1/destinations", cache.CacheTierModerate, 30 * time.Minute},
		// Research §23: Shipping
		{"shipping v1", "/shipping/v1/shipments/SHIP123", cache.CacheTierModerate, 15 * time.Minute},
		{"shipping v1 tracking", "/shipping/v1/tracking/TRACK123", cache.CacheTierModerate, 15 * time.Minute},
		{"shipping v2 tracking", "/shipping/v2/tracking", cache.CacheTierModerate, 15 * time.Minute},
		// Research §24: Solicitations
		{"solicitations", "/solicitations/v1/orders/123/solicitations", cache.CacheTierModerate, 1 * time.Hour},
		// Research §27: Easy Ship
		{"easyship", "/easyShip/2022-03-23/package", cache.CacheTierModerate, 10 * time.Minute},
		// Research §28: Messaging  -  GET endpoints are cacheable
		{"messaging actions", "/messaging/v1/orders/123/messages", cache.CacheTierModerate, 30 * time.Minute},
		// Research §29-30: Vendor
		{"vendor orders", "/vendor/orders/v1/purchaseOrders", cache.CacheTierModerate, 15 * time.Minute},
		{"vendor df orders", "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders", cache.CacheTierModerate, 15 * time.Minute},
		{"vendor df shipping", "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels", cache.CacheTierModerate, 15 * time.Minute},
		// Research §31: Data Kiosk
		{"datakiosk queries", "/datakiosk/2023-11-15/queries", cache.CacheTierModerate, 2 * time.Minute},
		// Research §15: Replenishment
		{"replenishment", "/replenishment/2022-11-07/sellingPartners/metrics/search", cache.CacheTierModerate, 1 * time.Hour},
		// AWD
		{"awd", "/awd/2024-05-09/inboundShipments", cache.CacheTierModerate, 10 * time.Minute},

		// ── Short tier (volatile data, pre-signed URLs) ─────────────
		// Research §5-6: Product Pricing
		{"pricing v0", "/products/pricing/v0/competitivePrice", cache.CacheTierShort, 60 * time.Second},
		{"pricing v2022", "/products/pricing/2022-05-01/items/competitiveSummary", cache.CacheTierShort, 60 * time.Second},
		// Research §7: Product Fees
		{"fees v0", "/products/fees/v0/listings/SKU1/feesEstimate", cache.CacheTierShort, 60 * time.Second},
		// Research §17: Orders
		{"orders list", "/orders/v0/orders", cache.CacheTierShort, 60 * time.Second},
		{"order single", "/orders/v0/orders/123-456", cache.CacheTierShort, 60 * time.Second},
		// Research §18: Report documents  -  pre-signed URL expires in 5min
		{"report documents", "/reports/2021-06-30/documents/DOC123", cache.CacheTierShort, 4 * time.Minute},
		// Research §19: Feed documents  -  pre-signed URL expires in 5min
		{"feed documents", "/feeds/2021-06-30/documents/DOC456", cache.CacheTierShort, 4 * time.Minute},
		// Research §31: Data Kiosk documents  -  pre-signed URL expires in 5min
		{"datakiosk documents", "/datakiosk/2023-11-15/documents/DOC789", cache.CacheTierShort, 4 * time.Minute},

		// ── Never tier (security-critical, write-only) ──────────────
		// Research §25: Tokens  -  never cache RDTs
		{"tokens", "/tokens/2021-03-01/restrictedDataToken", cache.CacheTierNever, 0},
		// Research §26: Uploads  -  pre-signed upload URLs
		{"uploads", "/uploads/2020-11-01/uploadDestinations/RESOURCE", cache.CacheTierNever, 0},
		// Research §32: Application Management  -  security-critical
		{"applications", "/applications/2023-11-30/clientSecret", cache.CacheTierNever, 0},
		// Authorization
		{"authorization", "/authorization/v1/authorizationCode", cache.CacheTierNever, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := tc.Classify("GET", tt.path, nil)
			assert.Equal(t, tt.tier, tier.Tier, "path: %s", tt.path)
			if tt.ttl > 0 {
				assert.Equal(t, tt.ttl, tier.DefaultTTL, "path: %s TTL", tt.path)
			}
		})
	}
}
