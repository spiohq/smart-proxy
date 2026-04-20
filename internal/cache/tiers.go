package cache

import (
	"net/http"
	"strings"
	"time"
)

// CacheTier represents the caching aggressiveness for an endpoint.
type CacheTier int

const (
	CacheTierAggressive CacheTier = 1 // 6-24 hours (catalog, definitions)
	CacheTierModerate   CacheTier = 2 // 5-30 minutes (reports, inventory, listings)
	CacheTierShort      CacheTier = 3 // 30-120 seconds (orders, pricing)
	CacheTierNever      CacheTier = 4 // No caching (feeds, notifications, mutations)
)

// TierConfig holds the resolved tier and TTL for a request.
type TierConfig struct {
	Tier           CacheTier
	DefaultTTL     time.Duration
	Reason         string // e.g. "PII_EXCLUDED"  -  empty for normal classification
	BatchCacheable bool   // true for POST batch endpoints (per-element caching)
	PostCacheable  bool   // true for single POST endpoints (body-hash caching)
}

// PIIChecker returns true if the request involves PII data.
// In Phase 3 this is nil (no PII checking). Phase 4 provides the real implementation.
type PIIChecker func(r *http.Request) bool

// TierClassifier maps endpoint patterns to cache tiers.
type TierClassifier struct {
	patterns   []tierPattern
	piiChecker PIIChecker
}

type tierPattern struct {
	prefix         string
	tier           CacheTier
	ttl            time.Duration
	batchCacheable bool // POST batch endpoints (per-element caching)
	postCacheable  bool // single POST endpoints (body-hash caching)
}

// defaultTierPatterns defines cache tiers for known SP-API endpoint prefixes.
// Order matters: more specific (longer) prefixes MUST come before broader ones
// because the classifier uses first-match semantics.
//
// Classifications are aligned with the comprehensive SP-API Caching Research
// (CACHING_RESEARCH.md) covering 33 API categories and ~200 endpoints.
var defaultTierPatterns = []tierPattern{
	// ── Never cache (security-critical, write-only) ─────────────────
	// Research §25: Tokens  -  RDTs are security-critical, short-lived
	{prefix: "/tokens/", tier: CacheTierNever, ttl: 0},
	// Research §26: Uploads  -  pre-signed upload URLs, must be unique
	{prefix: "/uploads/", tier: CacheTierNever, ttl: 0},
	// Research §32: Application Management  -  security-critical
	{prefix: "/applications/", tier: CacheTierNever, ttl: 0},
	// Authorization endpoints
	{prefix: "/authorization/", tier: CacheTierNever, ttl: 0},

	// ── Aggressive (stable reference data, 6h-24h) ──────────────────
	// Research §2: Product Type Definitions  -  schemas change extremely rarely
	{prefix: "/definitions/", tier: CacheTierAggressive, ttl: 24 * time.Hour},
	// Research §3: Catalog Items  -  stable catalog data
	{prefix: "/catalog/", tier: CacheTierAggressive, ttl: 12 * time.Hour},
	// Research §22: Sellers  -  quasi-static data, 0.016/s rate limit
	{prefix: "/sellers/", tier: CacheTierAggressive, ttl: 12 * time.Hour},
	// Research §10: FBA Inbound Eligibility  -  eligibility criteria rarely change
	{prefix: "/fba/inbound/eligibility/", tier: CacheTierModerate, ttl: 12 * time.Hour},

	// ── Short (pre-signed URL documents, TTL < 5min) ────────────────
	// These MUST come before their broader parent prefixes.
	// Research §18: Report documents  -  pre-signed URL expires in 5min
	{prefix: "/reports/2021-06-30/documents/", tier: CacheTierShort, ttl: 4 * time.Minute},
	// Research §19: Feed documents  -  pre-signed URL expires in 5min
	{prefix: "/feeds/2021-06-30/documents/", tier: CacheTierShort, ttl: 4 * time.Minute},
	// Research §31: Data Kiosk documents  -  pre-signed URL expires in 5min
	{prefix: "/datakiosk/2023-11-15/documents/", tier: CacheTierShort, ttl: 4 * time.Minute},

	// ── Moderate (5min-1h) ──────────────────────────────────────────
	// Research §18: Reports  -  report list/status
	{prefix: "/reports/", tier: CacheTierModerate, ttl: 10 * time.Minute},
	// Research §19: Feeds  -  GET feed list/status (cacheable, not Never!)
	{prefix: "/feeds/", tier: CacheTierModerate, ttl: 2 * time.Minute},
	// Research §1: Listings
	{prefix: "/listings/", tier: CacheTierModerate, ttl: 15 * time.Minute},
	// Research §4: A+ Content
	{prefix: "/aplus/", tier: CacheTierModerate, ttl: 30 * time.Minute},
	// Research §8: Sales
	{prefix: "/sales/", tier: CacheTierModerate, ttl: 15 * time.Minute},
	// Research §9: FBA Inventory  -  highly volatile, short TTL within Moderate
	{prefix: "/fba/inventory/", tier: CacheTierModerate, ttl: 5 * time.Minute},
	// Research §11: FBA Inbound
	{prefix: "/fba/inbound/", tier: CacheTierModerate, ttl: 10 * time.Minute},
	// Research §13: Fulfillment Outbound
	{prefix: "/fba/outbound/", tier: CacheTierModerate, ttl: 10 * time.Minute},
	// Research §12: FBA Small and Light (deprecated, returns NOT_ENROLLED)
	{prefix: "/fba/smallAndLight/", tier: CacheTierModerate, ttl: 24 * time.Hour},
	// Catch-all for other /fba/ endpoints
	{prefix: "/fba/", tier: CacheTierModerate, ttl: 10 * time.Minute},
	// Research §14: Merchant Fulfillment
	{prefix: "/mfn/", tier: CacheTierModerate, ttl: 15 * time.Minute},
	// Research §16: Supply Sources  -  1/s rate limit, data rarely changes
	{prefix: "/supplySources/", tier: CacheTierModerate, ttl: 1 * time.Hour},
	// Research §20: Finances  -  48h Amazon delay, inherently stale
	{prefix: "/finances/", tier: CacheTierModerate, ttl: 15 * time.Minute},
	// Research §21: Notifications  -  GET config endpoints are cacheable
	{prefix: "/notifications/", tier: CacheTierModerate, ttl: 30 * time.Minute},
	// Research §23: Shipping
	{prefix: "/shipping/", tier: CacheTierModerate, ttl: 15 * time.Minute},
	// Research §24: Solicitations
	{prefix: "/solicitations/", tier: CacheTierModerate, ttl: 1 * time.Hour},
	// Research §27: Easy Ship
	{prefix: "/easyShip/", tier: CacheTierModerate, ttl: 10 * time.Minute},
	// Research §28: Messaging  -  GET actions/attributes are cacheable
	{prefix: "/messaging/", tier: CacheTierModerate, ttl: 30 * time.Minute},
	// Research §29-30: Vendor APIs
	{prefix: "/vendor/", tier: CacheTierModerate, ttl: 15 * time.Minute},
	// Research §31: Data Kiosk  -  query list (documents handled above)
	{prefix: "/datakiosk/", tier: CacheTierModerate, ttl: 2 * time.Minute},
	// Research §15: Replenishment / Subscribe & Save
	{prefix: "/replenishment/", tier: CacheTierModerate, ttl: 1 * time.Hour},
	// AWD
	{prefix: "/awd/", tier: CacheTierModerate, ttl: 10 * time.Minute},

	// ── Batch-cacheable POST endpoints ──────────────────────────────
	// These POST batch endpoints carry deterministic payloads (arrays of
	// item/listing identifiers) so their responses are safe to cache.
	// getItemOffersBatch / getListingOffersBatch  -  5 min default TTL
	{prefix: "/batches/products/pricing/", tier: CacheTierModerate, ttl: 5 * time.Minute, batchCacheable: true},
	// getMyFeesEstimates (batch fees)  -  12 h default TTL
	{prefix: "/products/fees/v0/feesEstimate", tier: CacheTierAggressive, ttl: 12 * time.Hour, batchCacheable: true},
	// Single-item POST fees  -  12 h TTL (same rationale as batch fees)
	{prefix: "/products/fees/v0/items/", tier: CacheTierAggressive, ttl: 12 * time.Hour, postCacheable: true},
	{prefix: "/products/fees/v0/listings/", tier: CacheTierAggressive, ttl: 12 * time.Hour, postCacheable: true},

	// POST-cacheable read-only query endpoints
	// Shipping rates  -  4 h TTL (rates stable within hours)
	{prefix: "/shipping/v2/shipments/rates", tier: CacheTierModerate, ttl: 4 * time.Hour, postCacheable: true},
	{prefix: "/shipping/v1/rates", tier: CacheTierModerate, ttl: 4 * time.Hour, postCacheable: true},
	// Eligible shipping services  -  6 h TTL
	{prefix: "/mfn/v0/eligibleShippingServices", tier: CacheTierModerate, ttl: 6 * time.Hour, postCacheable: true},
	// Additional seller inputs  -  24 h TTL (requirement fields rarely change)
	{prefix: "/mfn/v0/additionalSellerInputs", tier: CacheTierAggressive, ttl: 24 * time.Hour, postCacheable: true},
	// FBA fulfillment preview  -  2 h TTL
	{prefix: "/fba/outbound/2020-07-01/fulfillmentOrders/preview", tier: CacheTierModerate, ttl: 2 * time.Hour, postCacheable: true},
	// EasyShip time slots  -  1 h TTL (time-sensitive)
	{prefix: "/easyShip/2022-03-23/timeSlot", tier: CacheTierModerate, ttl: 1 * time.Hour, postCacheable: true},
	// Replenishment search/metrics  -  6 h TTL (aggregated data)
	{prefix: "/replenishment/2022-11-07/sellingPartners/metrics/search", tier: CacheTierModerate, ttl: 6 * time.Hour, postCacheable: true},
	{prefix: "/replenishment/2022-11-07/offers/metrics/search", tier: CacheTierModerate, ttl: 6 * time.Hour, postCacheable: true},
	{prefix: "/replenishment/2022-11-07/offers/search", tier: CacheTierModerate, ttl: 6 * time.Hour, postCacheable: true},

	// ── Short (volatile business data, 30s-2min) ────────────────────
	// Research §17: Orders  -  0.0167/s rate limit, volatile
	{prefix: "/orders/", tier: CacheTierShort, ttl: 60 * time.Second},
	// Research §5-6: Product Pricing  -  highly volatile
	{prefix: "/products/pricing/", tier: CacheTierShort, ttl: 60 * time.Second},
	// Research §7: Product Fees — single-item endpoints (not the batch above)
	{prefix: "/products/fees/", tier: CacheTierShort, ttl: 60 * time.Second},
	// Catch-all for other /products/ endpoints
	{prefix: "/products/", tier: CacheTierShort, ttl: 60 * time.Second},
}

// NewTierClassifier creates a classifier with default SP-API tier patterns.
// piiChecker may be nil (Phase 3 default  -  no PII checking).
func NewTierClassifier(piiChecker PIIChecker) *TierClassifier {
	return &TierClassifier{
		patterns:   defaultTierPatterns,
		piiChecker: piiChecker,
	}
}

// Classify determines the cache tier for a given request.
// Non-GET methods are CacheTierNever unless the endpoint is batch-cacheable.
// If piiChecker returns true, returns CacheTierNever with Reason "PII_EXCLUDED".
func (tc *TierClassifier) Classify(method, path string, r *http.Request) TierConfig {
	if method != http.MethodGet {
		// Check if this is a cacheable POST before rejecting
		if method == http.MethodPost {
			for _, p := range tc.patterns {
				if (p.batchCacheable || p.postCacheable) && strings.HasPrefix(path, p.prefix) {
					return TierConfig{
						Tier: p.tier, DefaultTTL: p.ttl,
						BatchCacheable: p.batchCacheable,
						PostCacheable:  p.postCacheable,
					}
				}
			}
		}
		return TierConfig{Tier: CacheTierNever}
	}

	if tc.piiChecker != nil && r != nil && tc.piiChecker(r) {
		return TierConfig{Tier: CacheTierNever, Reason: "PII_EXCLUDED"}
	}

	for _, p := range tc.patterns {
		// Skip POST-only patterns when classifying GET requests
		if p.postCacheable || p.batchCacheable {
			continue
		}
		if strings.HasPrefix(path, p.prefix) {
			return TierConfig{Tier: p.tier, DefaultTTL: p.ttl}
		}
	}

	// Unknown endpoint: default to Short tier
	return TierConfig{Tier: CacheTierShort, DefaultTTL: 60 * time.Second}
}
