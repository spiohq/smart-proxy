package endpoint

import "strings"

// endpointPatterns maps static path prefixes to their parameterized patterns.
// segments is strings.Count(path, "/")+1, which includes the leading empty
// string equivalent from the leading slash.
// When suffix is non-empty, the path must also end with "/"+suffix to match.
// More-specific (longer/disambiguated) patterns must come before shorter ones
// sharing the same prefix.
var endpointPatterns = []struct {
	prefix   string
	segments int
	// suffix, when non-empty, must be the terminal path segment(s)  -  i.e. the
	// path ends with "/"+suffix. Suffix may contain slashes (multi-segment);
	// the segment count check ensures the full path still matches correctly.
	suffix  string // if non-empty, path must end with "/"+suffix
	pattern string
}{
	// ── Orders v0 ──────────────────────────────────────────────────────
	// 7 segments (most specific first)
	{"/orders/v0/orders/", 7, "orderItems/buyerInfo", "/orders/v0/orders/{orderId}/orderItems/buyerInfo"},
	// 6 segments (disambiguated by suffix)
	{"/orders/v0/orders/", 6, "orderItems", "/orders/v0/orders/{orderId}/orderItems"},
	{"/orders/v0/orders/", 6, "buyerInfo", "/orders/v0/orders/{orderId}/buyerInfo"},
	{"/orders/v0/orders/", 6, "address", "/orders/v0/orders/{orderId}/address"},
	{"/orders/v0/orders/", 6, "regulatedInfo", "/orders/v0/orders/{orderId}/regulatedInfo"},
	{"/orders/v0/orders/", 6, "shipmentConfirmation", "/orders/v0/orders/{orderId}/shipmentConfirmation"},
	{"/orders/v0/orders/", 6, "shipment", "/orders/v0/orders/{orderId}/shipment"},
	// 5 segments
	{"/orders/v0/orders/", 5, "", "/orders/v0/orders/{orderId}"},

	// ── Orders v2026-01-01 ─────────────────────────────────────────────
	{"/orders/2026-01-01/orders/", 5, "", "/orders/2026-01-01/orders/{orderId}"},

	// ── Catalog ────────────────────────────────────────────────────────
	{"/catalog/2022-04-01/items/", 5, "", "/catalog/2022-04-01/items/{asin}"},

	// ── Feeds ──────────────────────────────────────────────────────────
	{"/feeds/2021-06-30/feeds/", 5, "", "/feeds/2021-06-30/feeds/{feedId}"},
	{"/feeds/2021-06-30/documents/", 5, "", "/feeds/2021-06-30/documents/{feedDocumentId}"},

	// ── Reports ────────────────────────────────────────────────────────
	{"/reports/2021-06-30/reports/", 5, "", "/reports/2021-06-30/reports/{reportId}"},
	{"/reports/2021-06-30/schedules/", 5, "", "/reports/2021-06-30/schedules/{scheduleId}"},
	{"/reports/2021-06-30/documents/", 5, "", "/reports/2021-06-30/documents/{documentId}"},

	// ── Data Kiosk ─────────────────────────────────────────────────────
	{"/datakiosk/2023-11-15/queries/", 5, "", "/datakiosk/2023-11-15/queries/{queryId}"},
	{"/datakiosk/2023-11-15/documents/", 5, "", "/datakiosk/2023-11-15/documents/{documentId}"},

	// ── Listings Items ─────────────────────────────────────────────────
	// 6 segments before 5 (more specific first)
	{"/listings/2021-08-01/items/", 6, "", "/listings/2021-08-01/items/{sellerId}/{sku}"},
	{"/listings/2021-08-01/items/", 5, "", "/listings/2021-08-01/items/{sellerId}"},

	// ── Product Pricing v0 ─────────────────────────────────────────────
	{"/products/pricing/v0/items/", 7, "", "/products/pricing/v0/items/{asin}/offers"},
	{"/products/pricing/v0/listings/", 7, "", "/products/pricing/v0/listings/{sku}/offers"},

	// ── Product Fees v0 ────────────────────────────────────────────────
	{"/products/fees/v0/listings/", 7, "", "/products/fees/v0/listings/{sku}/feesEstimate"},
	{"/products/fees/v0/items/", 7, "", "/products/fees/v0/items/{asin}/feesEstimate"},

	// ── Product Type Definitions ───────────────────────────────────────
	{"/definitions/2020-09-01/productTypes/", 5, "", "/definitions/2020-09-01/productTypes/{productType}"},

	// ── FBA Inventory v1 ───────────────────────────────────────────────
	// "inventory" suffix must come before generic {sku} to avoid false match
	{"/fba/inventory/v1/items/", 6, "inventory", "/fba/inventory/v1/items/inventory"},
	{"/fba/inventory/v1/items/", 6, "", "/fba/inventory/v1/items/{sku}"},

	// ── Fulfillment Inbound v2024-03-20 ────────────────────────────────
	// 7 segments (most specific first)
	{"/fba/inbound/v2024-03-20/inboundPlans/", 7, "packingOptions", "/fba/inbound/v2024-03-20/inboundPlans/{planId}/packingOptions"},
	{"/fba/inbound/v2024-03-20/inboundPlans/", 7, "placementOptions", "/fba/inbound/v2024-03-20/inboundPlans/{planId}/placementOptions"},
	// 6 segments
	{"/fba/inbound/v2024-03-20/inboundPlans/", 6, "", "/fba/inbound/v2024-03-20/inboundPlans/{planId}"},

	// ── Fulfillment Inbound v0 (Legacy) ────────────────────────────────
	// 7 segments (disambiguated by suffix)
	{"/fba/inbound/v0/shipments/", 7, "labels", "/fba/inbound/v0/shipments/{shipmentId}/labels"},
	{"/fba/inbound/v0/shipments/", 7, "billOfLading", "/fba/inbound/v0/shipments/{shipmentId}/billOfLading"},
	{"/fba/inbound/v0/shipments/", 7, "items", "/fba/inbound/v0/shipments/{shipmentId}/items"},
	// 6 segments
	// Note: no generic {shipmentId} 6-seg entry  -  /fba/inbound/v0/shipments is a list endpoint

	// ── Fulfillment Outbound v2020-07-01 ───────────────────────────────
	// 8 segments (most specific first)
	{"/fba/outbound/2020-07-01/features/inventory/", 8, "", "/fba/outbound/2020-07-01/features/inventory/{featureName}/{sku}"},
	// 7 segments
	{"/fba/outbound/2020-07-01/fulfillmentOrders/", 7, "cancel", "/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}/cancel"},
	{"/fba/outbound/2020-07-01/fulfillmentOrders/", 7, "return", "/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}/return"},
	{"/fba/outbound/2020-07-01/features/inventory/", 7, "", "/fba/outbound/2020-07-01/features/inventory/{featureName}"},
	// 6 segments  -  "preview" suffix must come before generic {orderId}
	{"/fba/outbound/2020-07-01/fulfillmentOrders/", 6, "preview", "/fba/outbound/2020-07-01/fulfillmentOrders/preview"},
	{"/fba/outbound/2020-07-01/fulfillmentOrders/", 6, "", "/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}"},

	// ── Shipping v1 ────────────────────────────────────────────────────
	// 6 segments (disambiguated by suffix)
	{"/shipping/v1/shipments/", 6, "cancel", "/shipping/v1/shipments/{shipmentId}/cancel"},
	{"/shipping/v1/shipments/", 6, "purchaseLabels", "/shipping/v1/shipments/{shipmentId}/purchaseLabels"},
	{"/shipping/v1/containers/", 6, "label", "/shipping/v1/containers/{trackingId}/label"},
	// 5 segments
	{"/shipping/v1/shipments/", 5, "", "/shipping/v1/shipments/{shipmentId}"},
	{"/shipping/v1/tracking/", 5, "", "/shipping/v1/tracking/{trackingId}"},
	// 4 segments (list/POST endpoint -- more specific patterns above)
	{"/shipping/v1/shipments", 4, "", "/shipping/v1/shipments"},

	// ── Shipping v2 ────────────────────────────────────────────────────
	// 6 segments (disambiguated by suffix)
	{"/shipping/v2/shipments/", 6, "cancel", "/shipping/v2/shipments/{shipmentId}/cancel"},
	{"/shipping/v2/shipments/", 6, "documents", "/shipping/v2/shipments/{shipmentId}/documents"},
	// 5 segments (top-level operations on the shipments collection)
	{"/shipping/v2/shipments/rates", 5, "", "/shipping/v2/shipments/rates"},
	{"/shipping/v2/shipments/directPurchase", 5, "", "/shipping/v2/shipments/directPurchase"},
	// 4 segments (purchaseShipment list/POST endpoint -- more specific patterns above)
	{"/shipping/v2/shipments", 4, "", "/shipping/v2/shipments"},
	// Other top-level v2 operations (separate paths, not under /shipments)
	{"/shipping/v2/oneClickShipment", 4, "", "/shipping/v2/oneClickShipment"},

	// ── Merchant Fulfillment v0 ────────────────────────────────────────
	// 4 segments (list/POST endpoint -- before the 5-segment {shipmentId})
	{"/mfn/v0/shipments", 4, "", "/mfn/v0/shipments"},
	{"/mfn/v0/shipments/", 5, "", "/mfn/v0/shipments/{shipmentId}"},

	// ── EasyShip ───────────────────────────────────────────────────────
	{"/easyShip/2022-03-23/packages/bulk", 5, "", "/easyShip/2022-03-23/packages/bulk"},

	// ── Notifications v1 ───────────────────────────────────────────────
	{"/notifications/v1/subscriptions/", 5, "", "/notifications/v1/subscriptions/{type}"},
	{"/notifications/v1/destinations/", 5, "", "/notifications/v1/destinations/{destinationId}"},

	// ── Finances v0 ────────────────────────────────────────────────────
	{"/finances/v0/financialEventGroups/", 6, "financialEvents", "/finances/v0/financialEventGroups/{eventGroupId}/financialEvents"},
	{"/finances/v0/orders/", 6, "financialEvents", "/finances/v0/orders/{orderId}/financialEvents"},

	// ── Messaging v1 ───────────────────────────────────────────────────
	// 7 segments before 6 (more specific first)
	{"/messaging/v1/orders/", 7, "", "/messaging/v1/orders/{orderId}/messages/{messageId}"},
	{"/messaging/v1/orders/", 6, "", "/messaging/v1/orders/{orderId}/messages"},

	// ── Solicitations v1 ───────────────────────────────────────────────
	// 7 segments before 6 (more specific first)
	{"/solicitations/v1/orders/", 7, "productReviewAndSellerFeedback", "/solicitations/v1/orders/{orderId}/solicitations/productReviewAndSellerFeedback"},
	{"/solicitations/v1/orders/", 6, "solicitations", "/solicitations/v1/orders/{orderId}/solicitations"},

	// ── Uploads v2020-11-01 ────────────────────────────────────────────
	{"/uploads/2020-11-01/uploadDestinations/", 5, "", "/uploads/2020-11-01/uploadDestinations/{resource}"},

	// ── A+ Content v2020-11-01 ─────────────────────────────────────────
	{"/aplus/2020-11-01/contentDocuments/", 5, "", "/aplus/2020-11-01/contentDocuments/{contentReferenceKey}"},

	// ── AWD v2024-05-09 ────────────────────────────────────────────────
	{"/awd/2024-05-09/inboundShipments/", 5, "", "/awd/2024-05-09/inboundShipments/{shipmentId}"},

	// ── Supply Sources v2020-07-01 ─────────────────────────────────────
	{"/supplySources/2020-07-01/supplySources/", 5, "", "/supplySources/2020-07-01/supplySources/{supplySourceId}"},

	// ── Vendor Direct Fulfillment Orders ───────────────────────────────
	{"/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/", 7, "", "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/{purchaseOrderNumber}"},

	// ── Vendor Direct Fulfillment Shipping ─────────────────────────────
	{"/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/", 7, "", "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/{purchaseOrderNumber}"},
	{"/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/", 7, "", "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/{purchaseOrderNumber}"},
	{"/vendor/directFulfillment/shipping/2021-12-28/packingSlips/", 7, "", "/vendor/directFulfillment/shipping/2021-12-28/packingSlips/{purchaseOrderNumber}"},

	// ── Vendor Direct Fulfillment Transactions ─────────────────────────
	{"/vendor/directFulfillment/transactions/2021-12-28/transactions/", 7, "", "/vendor/directFulfillment/transactions/2021-12-28/transactions/{transactionId}"},

	// ── Vendor Orders v1 ───────────────────────────────────────────────
	{"/vendor/orders/v1/purchaseOrders/", 6, "", "/vendor/orders/v1/purchaseOrders/{purchaseOrderNumber}"},

	// ── Vendor Transaction Status v1 ───────────────────────────────────
	{"/vendor/transactionStatus/v1/transactions/", 6, "", "/vendor/transactionStatus/v1/transactions/{transactionId}"},
}

// Classify normalizes a request path to its canonical endpoint pattern.
// Query strings are stripped and trailing slashes are removed before matching.
// If no pattern matches, the cleaned path is returned as-is.
func Classify(path string) string {
	pattern, _ := ClassifyKnown(path)
	return pattern
}

// ClassifyKnown is like Classify but also reports whether the path matched a
// registered SP-API endpoint pattern. Callers that need fail-closed behavior
// (e.g. PII redaction) use ok=false to treat the path as unknown and apply
// strict defaults rather than falling back to "no rules apply".
func ClassifyKnown(path string) (pattern string, ok bool) {
	if idx := strings.IndexByte(path, '?'); idx != -1 {
		path = path[:idx]
	}
	path = strings.TrimRight(path, "/")

	segmentCount := strings.Count(path, "/") + 1
	for _, ep := range endpointPatterns {
		if strings.HasPrefix(path, ep.prefix) && segmentCount == ep.segments {
			if ep.suffix == "" || strings.HasSuffix(path, "/"+ep.suffix) {
				return ep.pattern, true
			}
		}
	}

	return path, false
}
