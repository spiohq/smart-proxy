package rdt

import "strings"

// PIIOperation describes a restricted SP-API operation that requires an RDT.
type PIIOperation struct {
	GenericPath  string   // Generic path form for RDT minting (e.g. "/orders/v0/orders/{orderId}")
	Method       string   // HTTP method (e.g. "GET", "POST")
	DataElements []string // Required dataElements for the RDT request (only Orders v0)
	Cacheable    bool     // Whether the RDT for this operation can be cached across requests
}

// RestrictedResource mirrors the Amazon Tokens API request structure.
type RestrictedResource struct {
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	DataElements []string `json:"dataElements,omitempty"`
}

// ToRestrictedResource converts a PIIOperation to the Amazon API request format.
func (op PIIOperation) ToRestrictedResource() RestrictedResource {
	return RestrictedResource{
		Method:       op.Method,
		Path:         op.GenericPath,
		DataElements: op.DataElements,
	}
}

// ordersV0DataElements are the three dataElements required for all Orders v0
// restricted endpoints. Amazon returns PII fields silently empty if any are
// missing, so we always send all three.
var ordersV0DataElements = []string{"buyerInfo", "shippingAddress", "buyerTaxInformation"}

// piiRule defines a single restricted endpoint pattern.
type piiRule struct {
	prefix      string
	segments    int
	suffix      string // if non-empty, path must end with "/"+suffix
	method      string // HTTP method ("GET", "POST", "PUT", or "*" for any)
	genericPath string
	dataElems   []string
	cacheable   bool
}

// piiRules is the complete table of SP-API operations that require RDTs.
// Order matters: more-specific patterns (more segments, suffix) before less-specific.
var piiRules = []piiRule{
	// ── Orders v0 ──────────────────────────────────────────────────────
	{"/orders/v0/orders/", 7, "orderItems/buyerInfo", "GET", "/orders/v0/orders/{orderId}/orderItems/buyerInfo", nil, true},
	{"/orders/v0/orders/", 6, "orderItems", "GET", "/orders/v0/orders/{orderId}/orderItems", ordersV0DataElements, true},
	{"/orders/v0/orders/", 6, "address", "GET", "/orders/v0/orders/{orderId}/address", nil, true},
	{"/orders/v0/orders/", 6, "buyerInfo", "GET", "/orders/v0/orders/{orderId}/buyerInfo", nil, true},
	{"/orders/v0/orders/", 6, "regulatedInfo", "GET", "/orders/v0/orders/{orderId}/regulatedInfo", nil, true},
	{"/orders/v0/orders/", 5, "", "GET", "/orders/v0/orders/{orderId}", ordersV0DataElements, true},
	// getOrders (list endpoint, no trailing ID segment)
	{"/orders/v0/orders", 4, "", "GET", "/orders/v0/orders", ordersV0DataElements, true},

	// ── Merchant Fulfillment (MFN) v0 ──────────────────────────────────
	{"/mfn/v0/shipments/", 6, "cancel", "PUT", "/mfn/v0/shipments/{shipmentId}/cancel", nil, true},
	{"/mfn/v0/shipments/", 5, "", "GET", "/mfn/v0/shipments/{shipmentId}", nil, true},
	{"/mfn/v0/shipments", 4, "", "POST", "/mfn/v0/shipments", nil, true},

	// ── Shipping v1 ────────────────────────────────────────────────────
	{"/shipping/v1/shipments/", 5, "", "GET", "/shipping/v1/shipments/{shipmentId}", nil, true},

	// ── Shipment Invoicing (Brazil) ────────────────────────────────────
	{"/fba/outbound/brazil/v0/shipments/", 7, "", "GET", "/fba/outbound/brazil/v0/shipments/{shipmentId}", nil, true},

	// ── Easy Ship ──────────────────────────────────────────────────────
	{"/easyShip/2022-03-23/packages/bulk", 5, "", "POST", "/easyShip/2022-03-23/packages/bulk", nil, true},

	// ── Direct Fulfillment Orders ──────────────────────────────────────
	{"/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/", 7, "", "GET", "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/{purchaseOrderNumber}", nil, true},
	{"/vendor/directFulfillment/orders/2021-12-28/purchaseOrders", 6, "", "GET", "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders", nil, true},

	// ── Direct Fulfillment Shipping ────────────────────────────────────
	{"/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/", 7, "", "GET", "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/{purchaseOrderNumber}", nil, true},
	{"/vendor/directFulfillment/shipping/2021-12-28/shippingLabels", 6, "", "GET", "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels", nil, true},
	{"/vendor/directFulfillment/shipping/2021-12-28/packingSlips/", 7, "", "GET", "/vendor/directFulfillment/shipping/2021-12-28/packingSlips/{purchaseOrderNumber}", nil, true},
	{"/vendor/directFulfillment/shipping/2021-12-28/packingSlips", 6, "", "GET", "/vendor/directFulfillment/shipping/2021-12-28/packingSlips", nil, true},
	{"/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/", 7, "", "GET", "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/{purchaseOrderNumber}", nil, true},
	{"/vendor/directFulfillment/shipping/2021-12-28/customerInvoices", 6, "", "GET", "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices", nil, true},

	// Note: Report documents (GET /reports/2021-06-30/documents/{docId}) are NOT
	// in this table. They are handled separately by the ReportTracker because:
	// - Amazon requires the concrete documentId in the RDT path (no generic form)
	// - Only 16 specific reportTypes are restricted (need sniffing to determine)
	// - The proxy must correlate POST /reports -> GET /reports/{id} -> GET /documents/{docId}
}

// MatchPIIOperation checks whether the given HTTP method and path correspond
// to a restricted SP-API operation that requires an RDT. If matched, it
// returns the PIIOperation describing the generic path, required dataElements,
// and cacheability. Query strings and trailing slashes are stripped before
// matching.
func MatchPIIOperation(method, path string) (PIIOperation, bool) {
	// Strip query string
	if idx := strings.IndexByte(path, '?'); idx != -1 {
		path = path[:idx]
	}
	// Strip trailing slash
	path = strings.TrimRight(path, "/")

	segmentCount := strings.Count(path, "/") + 1

	for _, rule := range piiRules {
		if rule.method != "*" && rule.method != method {
			continue
		}
		if !strings.HasPrefix(path, rule.prefix) && path != rule.prefix {
			continue
		}
		if segmentCount != rule.segments {
			continue
		}
		if rule.suffix != "" && !strings.HasSuffix(path, "/"+rule.suffix) {
			continue
		}
		return PIIOperation{
			GenericPath:  rule.genericPath,
			Method:       rule.method,
			DataElements: rule.dataElems,
			Cacheable:    rule.cacheable,
		}, true
	}
	return PIIOperation{}, false
}
