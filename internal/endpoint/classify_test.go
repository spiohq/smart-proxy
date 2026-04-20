package endpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// ── Static list endpoints (pass through as-is) ─────────────────
		{
			name:     "orders v0 list",
			input:    "/orders/v0/orders",
			expected: "/orders/v0/orders",
		},
		{
			name:     "orders v2026 list",
			input:    "/orders/2026-01-01/orders",
			expected: "/orders/2026-01-01/orders",
		},
		{
			name:     "catalog items list",
			input:    "/catalog/2022-04-01/items",
			expected: "/catalog/2022-04-01/items",
		},
		{
			name:     "feeds list",
			input:    "/feeds/2021-06-30/feeds",
			expected: "/feeds/2021-06-30/feeds",
		},
		{
			name:     "feeds documents list",
			input:    "/feeds/2021-06-30/documents",
			expected: "/feeds/2021-06-30/documents",
		},
		{
			name:     "reports list",
			input:    "/reports/2021-06-30/reports",
			expected: "/reports/2021-06-30/reports",
		},
		{
			name:     "reports schedules list",
			input:    "/reports/2021-06-30/schedules",
			expected: "/reports/2021-06-30/schedules",
		},
		{
			name:     "datakiosk queries list",
			input:    "/datakiosk/2023-11-15/queries",
			expected: "/datakiosk/2023-11-15/queries",
		},
		{
			name:     "financial events list",
			input:    "/finances/v0/financialEvents",
			expected: "/finances/v0/financialEvents",
		},
		{
			name:     "financial event groups list",
			input:    "/finances/v0/financialEventGroups",
			expected: "/finances/v0/financialEventGroups",
		},
		{
			name:     "notifications subscriptions list",
			input:    "/notifications/v1/subscriptions",
			expected: "/notifications/v1/subscriptions",
		},
		{
			name:     "notifications destinations list",
			input:    "/notifications/v1/destinations",
			expected: "/notifications/v1/destinations",
		},
		{
			name:     "fba inventory summaries",
			input:    "/fba/inventory/v1/summaries",
			expected: "/fba/inventory/v1/summaries",
		},
		{
			name:     "fba inventory items list",
			input:    "/fba/inventory/v1/items",
			expected: "/fba/inventory/v1/items",
		},
		{
			name:     "fba inbound plans list",
			input:    "/fba/inbound/v2024-03-20/inboundPlans",
			expected: "/fba/inbound/v2024-03-20/inboundPlans",
		},
		{
			name:     "fba inbound v0 shipments list",
			input:    "/fba/inbound/v0/shipments",
			expected: "/fba/inbound/v0/shipments",
		},
		{
			name:     "fba outbound fulfillment orders list",
			input:    "/fba/outbound/2020-07-01/fulfillmentOrders",
			expected: "/fba/outbound/2020-07-01/fulfillmentOrders",
		},
		{
			name:     "shipping v1 shipments list",
			input:    "/shipping/v1/shipments",
			expected: "/shipping/v1/shipments",
		},
		{
			name:     "shipping v2 shipments list",
			input:    "/shipping/v2/shipments",
			expected: "/shipping/v2/shipments",
		},
		{
			name:     "mfn shipments list",
			input:    "/mfn/v0/shipments",
			expected: "/mfn/v0/shipments",
		},
		{
			name:     "definitions product types list",
			input:    "/definitions/2020-09-01/productTypes",
			expected: "/definitions/2020-09-01/productTypes",
		},
		{
			name:     "listings restrictions list",
			input:    "/listings/2021-08-01/restrictions",
			expected: "/listings/2021-08-01/restrictions",
		},
		{
			name:     "aplus content documents list",
			input:    "/aplus/2020-11-01/contentDocuments",
			expected: "/aplus/2020-11-01/contentDocuments",
		},
		{
			name:     "awd inbound shipments list",
			input:    "/awd/2024-05-09/inboundShipments",
			expected: "/awd/2024-05-09/inboundShipments",
		},
		{
			name:     "supply sources list",
			input:    "/supplySources/2020-07-01/supplySources",
			expected: "/supplySources/2020-07-01/supplySources",
		},
		{
			name:     "vendor df orders list",
			input:    "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders",
			expected: "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders",
		},
		{
			name:     "vendor df shipping labels list",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels",
		},
		{
			name:     "vendor df customer invoices list",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices",
		},
		{
			name:     "vendor df packing slips list",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/packingSlips",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/packingSlips",
		},
		{
			name:     "vendor orders list",
			input:    "/vendor/orders/v1/purchaseOrders",
			expected: "/vendor/orders/v1/purchaseOrders",
		},

		// ── Orders v0 (parameterized) ──────────────────────────────────
		{
			name:     "order by id",
			input:    "/orders/v0/orders/902-1845369-9154065",
			expected: "/orders/v0/orders/{orderId}",
		},
		{
			name:     "order items",
			input:    "/orders/v0/orders/902-1845369-9154065/orderItems",
			expected: "/orders/v0/orders/{orderId}/orderItems",
		},
		{
			name:     "order buyer info",
			input:    "/orders/v0/orders/902-1845369-9154065/buyerInfo",
			expected: "/orders/v0/orders/{orderId}/buyerInfo",
		},
		{
			name:     "order address",
			input:    "/orders/v0/orders/902-1845369-9154065/address",
			expected: "/orders/v0/orders/{orderId}/address",
		},
		{
			name:     "order items buyer info",
			input:    "/orders/v0/orders/902-1845369-9154065/orderItems/buyerInfo",
			expected: "/orders/v0/orders/{orderId}/orderItems/buyerInfo",
		},
		{
			name:     "order regulated info",
			input:    "/orders/v0/orders/902-1845369-9154065/regulatedInfo",
			expected: "/orders/v0/orders/{orderId}/regulatedInfo",
		},
		{
			name:     "order shipment confirmation",
			input:    "/orders/v0/orders/902-1845369-9154065/shipmentConfirmation",
			expected: "/orders/v0/orders/{orderId}/shipmentConfirmation",
		},
		{
			name:     "order shipment",
			input:    "/orders/v0/orders/902-1845369-9154065/shipment",
			expected: "/orders/v0/orders/{orderId}/shipment",
		},

		// ── Orders v2026-01-01 ─────────────────────────────────────────
		{
			name:     "order v2026 by id",
			input:    "/orders/2026-01-01/orders/902-1845369-9154065",
			expected: "/orders/2026-01-01/orders/{orderId}",
		},

		// ── Catalog Items ──────────────────────────────────────────────
		{
			name:     "catalog item by asin",
			input:    "/catalog/2022-04-01/items/B08N5WRWNW",
			expected: "/catalog/2022-04-01/items/{asin}",
		},

		// ── Feeds ──────────────────────────────────────────────────────
		{
			name:     "feed by id",
			input:    "/feeds/2021-06-30/feeds/50009018609",
			expected: "/feeds/2021-06-30/feeds/{feedId}",
		},
		{
			name:     "feed document by id",
			input:    "/feeds/2021-06-30/documents/amzn1.tortuga.3.ed4cd0ce-447A-1234-A1BC-EXAMPLE12345",
			expected: "/feeds/2021-06-30/documents/{feedDocumentId}",
		},

		// ── Reports ────────────────────────────────────────────────────
		{
			name:     "report by id",
			input:    "/reports/2021-06-30/reports/RPT-67890",
			expected: "/reports/2021-06-30/reports/{reportId}",
		},
		{
			name:     "report schedule by id",
			input:    "/reports/2021-06-30/schedules/SCHED-11111",
			expected: "/reports/2021-06-30/schedules/{scheduleId}",
		},
		{
			name:     "report document by id",
			input:    "/reports/2021-06-30/documents/DOC-44444",
			expected: "/reports/2021-06-30/documents/{documentId}",
		},

		// ── Data Kiosk ─────────────────────────────────────────────────
		{
			name:     "datakiosk query by id",
			input:    "/datakiosk/2023-11-15/queries/QRY-abcdef-12345",
			expected: "/datakiosk/2023-11-15/queries/{queryId}",
		},
		{
			name:     "datakiosk document by id",
			input:    "/datakiosk/2023-11-15/documents/DOC-zzzzzz-99999",
			expected: "/datakiosk/2023-11-15/documents/{documentId}",
		},

		// ── Listings Items ─────────────────────────────────────────────
		{
			name:     "listing by seller and sku",
			input:    "/listings/2021-08-01/items/ATVPDKIKX0DER/MY-SKU-123",
			expected: "/listings/2021-08-01/items/{sellerId}/{sku}",
		},
		{
			name:     "listings by seller",
			input:    "/listings/2021-08-01/items/ATVPDKIKX0DER",
			expected: "/listings/2021-08-01/items/{sellerId}",
		},

		// ── Product Pricing v0 ─────────────────────────────────────────
		{
			name:     "product pricing offers by asin",
			input:    "/products/pricing/v0/items/B09V3KXJPB/offers",
			expected: "/products/pricing/v0/items/{asin}/offers",
		},
		{
			name:     "product pricing offers by sku",
			input:    "/products/pricing/v0/listings/SKU-ALPHA-007/offers",
			expected: "/products/pricing/v0/listings/{sku}/offers",
		},

		// ── Product Fees v0 ────────────────────────────────────────────
		{
			name:     "product fees estimate by sku",
			input:    "/products/fees/v0/listings/SKU-ALPHA-007/feesEstimate",
			expected: "/products/fees/v0/listings/{sku}/feesEstimate",
		},
		{
			name:     "product fees estimate by asin",
			input:    "/products/fees/v0/items/B09V3KXJPB/feesEstimate",
			expected: "/products/fees/v0/items/{asin}/feesEstimate",
		},

		// ── Product Type Definitions ───────────────────────────────────
		{
			name:     "product type definition by type",
			input:    "/definitions/2020-09-01/productTypes/LUGGAGE",
			expected: "/definitions/2020-09-01/productTypes/{productType}",
		},

		// ── FBA Inventory v1 ───────────────────────────────────────────
		{
			name:     "fba inventory items inventory (static suffix)",
			input:    "/fba/inventory/v1/items/inventory",
			expected: "/fba/inventory/v1/items/inventory",
		},
		{
			name:     "fba inventory item by sku",
			input:    "/fba/inventory/v1/items/SKU-BETA-42",
			expected: "/fba/inventory/v1/items/{sku}",
		},

		// ── Fulfillment Inbound v2024-03-20 ────────────────────────────
		{
			name:     "inbound plan by id",
			input:    "/fba/inbound/v2024-03-20/inboundPlans/PLAN-abc123",
			expected: "/fba/inbound/v2024-03-20/inboundPlans/{planId}",
		},
		{
			name:     "inbound plan packing options",
			input:    "/fba/inbound/v2024-03-20/inboundPlans/PLAN-abc123/packingOptions",
			expected: "/fba/inbound/v2024-03-20/inboundPlans/{planId}/packingOptions",
		},
		{
			name:     "inbound plan placement options",
			input:    "/fba/inbound/v2024-03-20/inboundPlans/PLAN-abc123/placementOptions",
			expected: "/fba/inbound/v2024-03-20/inboundPlans/{planId}/placementOptions",
		},

		// ── Fulfillment Inbound v0 (Legacy) ────────────────────────────
		{
			name:     "inbound v0 shipment labels",
			input:    "/fba/inbound/v0/shipments/FBA15DJ9SVTH/labels",
			expected: "/fba/inbound/v0/shipments/{shipmentId}/labels",
		},
		{
			name:     "inbound v0 shipment bill of lading",
			input:    "/fba/inbound/v0/shipments/FBA15DJ9SVTH/billOfLading",
			expected: "/fba/inbound/v0/shipments/{shipmentId}/billOfLading",
		},
		{
			name:     "inbound v0 shipment items",
			input:    "/fba/inbound/v0/shipments/FBA15DJ9SVTH/items",
			expected: "/fba/inbound/v0/shipments/{shipmentId}/items",
		},

		// ── Fulfillment Outbound v2020-07-01 ───────────────────────────
		{
			name:     "fulfillment order by id",
			input:    "/fba/outbound/2020-07-01/fulfillmentOrders/FO-12345",
			expected: "/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}",
		},
		{
			name:     "fulfillment order preview (static suffix)",
			input:    "/fba/outbound/2020-07-01/fulfillmentOrders/preview",
			expected: "/fba/outbound/2020-07-01/fulfillmentOrders/preview",
		},
		{
			name:     "fulfillment order cancel",
			input:    "/fba/outbound/2020-07-01/fulfillmentOrders/FO-12345/cancel",
			expected: "/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}/cancel",
		},
		{
			name:     "fulfillment order return",
			input:    "/fba/outbound/2020-07-01/fulfillmentOrders/FO-12345/return",
			expected: "/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}/return",
		},
		{
			name:     "fulfillment features inventory by feature name",
			input:    "/fba/outbound/2020-07-01/features/inventory/AFN_FILLABLE",
			expected: "/fba/outbound/2020-07-01/features/inventory/{featureName}",
		},
		{
			name:     "fulfillment features inventory by feature name and sku",
			input:    "/fba/outbound/2020-07-01/features/inventory/AFN_FILLABLE/SKU-99",
			expected: "/fba/outbound/2020-07-01/features/inventory/{featureName}/{sku}",
		},

		// ── Shipping v1 ────────────────────────────────────────────────
		{
			name:     "shipping v1 shipment by id",
			input:    "/shipping/v1/shipments/SHIP-v1-00001",
			expected: "/shipping/v1/shipments/{shipmentId}",
		},
		{
			name:     "shipping v1 shipment cancel",
			input:    "/shipping/v1/shipments/SHIP-v1-00001/cancel",
			expected: "/shipping/v1/shipments/{shipmentId}/cancel",
		},
		{
			name:     "shipping v1 shipment purchase labels",
			input:    "/shipping/v1/shipments/SHIP-v1-00001/purchaseLabels",
			expected: "/shipping/v1/shipments/{shipmentId}/purchaseLabels",
		},
		{
			name:     "shipping v1 container label",
			input:    "/shipping/v1/containers/1Z999AA10123456784/label",
			expected: "/shipping/v1/containers/{trackingId}/label",
		},
		{
			name:     "shipping v1 tracking by id",
			input:    "/shipping/v1/tracking/1Z999AA10123456784",
			expected: "/shipping/v1/tracking/{trackingId}",
		},

		// ── Shipping v2 ────────────────────────────────────────────────
		{
			name:     "shipping v2 shipment cancel",
			input:    "/shipping/v2/shipments/SHIP-v2-00001/cancel",
			expected: "/shipping/v2/shipments/{shipmentId}/cancel",
		},
		{
			name:     "shipping v2 shipment documents",
			input:    "/shipping/v2/shipments/SHIP-v2-00001/documents",
			expected: "/shipping/v2/shipments/{shipmentId}/documents",
		},
		{
			name:     "shipping v2 shipment direct purchase",
			input:    "/shipping/v2/shipments/SHIP-v2-00001/directPurchase",
			expected: "/shipping/v2/shipments/{shipmentId}/directPurchase",
		},

		// ── Merchant Fulfillment v0 ────────────────────────────────────
		{
			name:     "mfn shipment by id",
			input:    "/mfn/v0/shipments/SHIP-MFN-12345",
			expected: "/mfn/v0/shipments/{shipmentId}",
		},

		// ── Notifications v1 ───────────────────────────────────────────
		{
			name:     "notification subscription by type",
			input:    "/notifications/v1/subscriptions/BRANDED_ITEM_CONTENT_CHANGE",
			expected: "/notifications/v1/subscriptions/{type}",
		},
		{
			name:     "notification destination by id",
			input:    "/notifications/v1/destinations/DEST-abc-123",
			expected: "/notifications/v1/destinations/{destinationId}",
		},

		// ── Finances v0 ────────────────────────────────────────────────
		{
			name:     "finances event group financial events",
			input:    "/finances/v0/financialEventGroups/GRP-56789/financialEvents",
			expected: "/finances/v0/financialEventGroups/{eventGroupId}/financialEvents",
		},
		{
			name:     "finances order financial events",
			input:    "/finances/v0/orders/902-1845369-9154065/financialEvents",
			expected: "/finances/v0/orders/{orderId}/financialEvents",
		},

		// ── Messaging v1 ───────────────────────────────────────────────
		{
			name:     "messaging messages list",
			input:    "/messaging/v1/orders/902-1845369-9154065/messages",
			expected: "/messaging/v1/orders/{orderId}/messages",
		},
		{
			name:     "messaging message by id",
			input:    "/messaging/v1/orders/902-1845369-9154065/messages/MSG-00001",
			expected: "/messaging/v1/orders/{orderId}/messages/{messageId}",
		},

		// ── Solicitations v1 ───────────────────────────────────────────
		{
			name:     "solicitations list",
			input:    "/solicitations/v1/orders/902-1845369-9154065/solicitations",
			expected: "/solicitations/v1/orders/{orderId}/solicitations",
		},
		{
			name:     "solicitations product review and seller feedback",
			input:    "/solicitations/v1/orders/902-1845369-9154065/solicitations/productReviewAndSellerFeedback",
			expected: "/solicitations/v1/orders/{orderId}/solicitations/productReviewAndSellerFeedback",
		},

		// ── Uploads v2020-11-01 ────────────────────────────────────────
		{
			name:     "upload destination by resource",
			input:    "/uploads/2020-11-01/uploadDestinations/contentDoc123",
			expected: "/uploads/2020-11-01/uploadDestinations/{resource}",
		},

		// ── A+ Content v2020-11-01 ─────────────────────────────────────
		{
			name:     "aplus content document by key",
			input:    "/aplus/2020-11-01/contentDocuments/CREF-KEY-ABC123",
			expected: "/aplus/2020-11-01/contentDocuments/{contentReferenceKey}",
		},

		// ── AWD v2024-05-09 ────────────────────────────────────────────
		{
			name:     "awd inbound shipment by id",
			input:    "/awd/2024-05-09/inboundShipments/SHIP-AWD-99999",
			expected: "/awd/2024-05-09/inboundShipments/{shipmentId}",
		},

		// ── Supply Sources v2020-07-01 ─────────────────────────────────
		{
			name:     "supply source by id",
			input:    "/supplySources/2020-07-01/supplySources/SRC-abc-456",
			expected: "/supplySources/2020-07-01/supplySources/{supplySourceId}",
		},

		// ── Vendor Direct Fulfillment Orders ───────────────────────────
		{
			name:     "vendor df purchase order by number",
			input:    "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/2JK3S9VC",
			expected: "/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/{purchaseOrderNumber}",
		},

		// ── Vendor Direct Fulfillment Shipping ─────────────────────────
		{
			name:     "vendor df shipping label by po number",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/2JK3S9VC",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/{purchaseOrderNumber}",
		},
		{
			name:     "vendor df customer invoice by po number",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/2JK3S9VC",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/{purchaseOrderNumber}",
		},
		{
			name:     "vendor df packing slip by po number",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/packingSlips/2JK3S9VC",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/packingSlips/{purchaseOrderNumber}",
		},

		// ── Vendor Direct Fulfillment Transactions ─────────────────────
		{
			name:     "vendor df transaction by id",
			input:    "/vendor/directFulfillment/transactions/2021-12-28/transactions/TXN-20210101-001",
			expected: "/vendor/directFulfillment/transactions/2021-12-28/transactions/{transactionId}",
		},

		// ── Vendor Orders v1 ───────────────────────────────────────────
		{
			name:     "vendor purchase order by number",
			input:    "/vendor/orders/v1/purchaseOrders/4Z32PABC",
			expected: "/vendor/orders/v1/purchaseOrders/{purchaseOrderNumber}",
		},

		// ── Vendor Transaction Status v1 ───────────────────────────────
		{
			name:     "vendor transaction status by id",
			input:    "/vendor/transactionStatus/v1/transactions/TXN-20210101-002",
			expected: "/vendor/transactionStatus/v1/transactions/{transactionId}",
		},

		// ── Edge cases ─────────────────────────────────────────────────
		{
			name:     "query string stripped on list endpoint",
			input:    "/orders/v0/orders?MarketplaceIds=A1PA6795UKMFR9",
			expected: "/orders/v0/orders",
		},
		{
			name:     "query string stripped for parameterized path",
			input:    "/orders/v0/orders/902-1845369-9154065?MarketplaceIds=A1F83G8C2ARO7P",
			expected: "/orders/v0/orders/{orderId}",
		},
		{
			name:     "query string stripped on new vendor endpoint",
			input:    "/vendor/orders/v1/purchaseOrders?createdAfter=2025-01-01",
			expected: "/vendor/orders/v1/purchaseOrders",
		},
		{
			name:     "query string stripped on shipping v2 cancel",
			input:    "/shipping/v2/shipments/SHIP-v2-00001/cancel?key=val",
			expected: "/shipping/v2/shipments/{shipmentId}/cancel",
		},
		{
			name:     "trailing slash stripped and matched",
			input:    "/orders/v0/orders/902-1845369-9154065/",
			expected: "/orders/v0/orders/{orderId}",
		},
		{
			name:     "trailing slash on list endpoint",
			input:    "/catalog/2022-04-01/items/",
			expected: "/catalog/2022-04-01/items",
		},
		{
			name:     "trailing slash on vendor df shipping labels",
			input:    "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/",
			expected: "/vendor/directFulfillment/shipping/2021-12-28/shippingLabels",
		},
		{
			name:     "trailing slash on fba outbound features inventory",
			input:    "/fba/outbound/2020-07-01/features/inventory/AFN_FILLABLE/",
			expected: "/fba/outbound/2020-07-01/features/inventory/{featureName}",
		},
		{
			name:     "unknown path returned as-is",
			input:    "/new-api/2027-01-01/widgets/123",
			expected: "/new-api/2027-01-01/widgets/123",
		},
		{
			name:     "unknown path with query string",
			input:    "/unknown/v1/resource?foo=bar",
			expected: "/unknown/v1/resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}
