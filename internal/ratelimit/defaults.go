package ratelimit

import "github.com/spiohq/smart-proxy/internal/endpoint"

// BucketParams defines the rate and burst capacity for a token bucket.
type BucketParams struct {
	Rate  float64 // Tokens per second
	Burst float64 // Max bucket capacity
}

// DefaultBucketParams contains published SP-API rate limits per endpoint pattern.
// These are the default (method-agnostic) limits applied per selling-partner + application pair.
// For endpoints where different HTTP methods have different rate/burst values,
// see MethodBucketOverrides.
//
// Source: Amazon SP-API documentation, verified March 2026.
var DefaultBucketParams = map[string]BucketParams{
	// ── Orders API v0 ──────────────────────────────────────────────────
	// Dynamic usage plans  -  values below are defaults; high-volume sellers get more.
	"/orders/v0/orders":                                {Rate: 0.0167, Burst: 20},  // getOrders ~1 req/min
	"/orders/v0/orders/{orderId}":                      {Rate: 0.5, Burst: 30},     // getOrder
	"/orders/v0/orders/{orderId}/orderItems":           {Rate: 0.5, Burst: 30},     // getOrderItems
	"/orders/v0/orders/{orderId}/buyerInfo":            {Rate: 0.5, Burst: 30},     // getOrderBuyerInfo (RDT)
	"/orders/v0/orders/{orderId}/address":              {Rate: 0.5, Burst: 30},     // getOrderAddress (RDT)
	"/orders/v0/orders/{orderId}/orderItems/buyerInfo": {Rate: 0.5, Burst: 30},     // getOrderItemsBuyerInfo (RDT)
	"/orders/v0/orders/{orderId}/regulatedInfo":        {Rate: 0.5, Burst: 30},     // getOrderRegulatedInfo / updateVerificationStatus
	"/orders/v0/orders/{orderId}/shipmentConfirmation": {Rate: 2.0, Burst: 10},     // confirmShipment (POST)
	"/orders/v0/orders/{orderId}/shipment":             {Rate: 5.0, Burst: 15},     // updateShipmentStatus (POST)

	// ── Orders API v2026-01-01 ─────────────────────────────────────────
	"/orders/2026-01-01/orders":           {Rate: 0.0056, Burst: 20}, // searchOrders ~1 req/3min  -  more restrictive than v0
	"/orders/2026-01-01/orders/{orderId}": {Rate: 0.5, Burst: 30},    // getOrder

	// ── Catalog Items API v2022-04-01 ──────────────────────────────────
	// Pair-level: Rate 2, Burst 2. App-level: see AppBucketParams.
	"/catalog/2022-04-01/items":        {Rate: 2.0, Burst: 2},
	"/catalog/2022-04-01/items/{asin}": {Rate: 2.0, Burst: 2},

	// ── Feeds API v2021-06-30 ──────────────────────────────────────────
	"/feeds/2021-06-30/feeds":                      {Rate: 0.0222, Burst: 10}, // getFeeds ~1 req/45s
	"/feeds/2021-06-30/feeds/{feedId}":             {Rate: 2.0, Burst: 15},    // getFeed / cancelFeed
	"/feeds/2021-06-30/documents":                  {Rate: 0.5, Burst: 15},    // createFeedDocument
	"/feeds/2021-06-30/documents/{feedDocumentId}": {Rate: 0.0222, Burst: 10}, // getFeedDocument

	// ── Reports API v2021-06-30 ────────────────────────────────────────
	"/reports/2021-06-30/reports":                {Rate: 0.0222, Burst: 10}, // getReports
	"/reports/2021-06-30/reports/{reportId}":     {Rate: 2.0, Burst: 15},   // getReport
	"/reports/2021-06-30/schedules":              {Rate: 0.0222, Burst: 10}, // getReportSchedules / createReportSchedule
	"/reports/2021-06-30/schedules/{scheduleId}": {Rate: 0.0222, Burst: 10}, // getReportSchedule / cancelReportSchedule
	"/reports/2021-06-30/documents/{documentId}": {Rate: 0.0167, Burst: 15}, // getReportDocument

	// ── Data Kiosk API v2023-11-15 ─────────────────────────────────────
	"/datakiosk/2023-11-15/queries":                {Rate: 0.0222, Burst: 10}, // getQueries / cancelQuery
	"/datakiosk/2023-11-15/queries/{queryId}":      {Rate: 2.0, Burst: 15},    // getQuery
	"/datakiosk/2023-11-15/documents/{documentId}": {Rate: 0.0167, Burst: 15}, // getDocument

	// ── Listings Items API v2021-08-01 ─────────────────────────────────
	// Pair-level: Rate 5, Burst 5 for all methods. App-level limits vary  -  see AppBucketParams.
	"/listings/2021-08-01/items/{sellerId}/{sku}": {Rate: 5.0, Burst: 5},
	"/listings/2021-08-01/items/{sellerId}":       {Rate: 5.0, Burst: 5}, // searchListingsItems

	// ── Listings Restrictions API v2021-08-01 ──────────────────────────
	"/listings/2021-08-01/restrictions": {Rate: 5.0, Burst: 10},

	// ── Product Pricing API v0 ─────────────────────────────────────────
	"/products/pricing/v0/competitivePrice":      {Rate: 0.5, Burst: 1}, // getCompetitivePricing (up to 20 ASINs/SKUs)
	"/products/pricing/v0/price":                 {Rate: 0.5, Burst: 1}, // getPricing (up to 20 ASINs/SKUs)
	"/products/pricing/v0/items/{asin}/offers":   {Rate: 0.5, Burst: 1}, // getItemOffers
	"/products/pricing/v0/listings/{sku}/offers": {Rate: 0.5, Burst: 1}, // getListingOffers
	"/batches/products/pricing/v0/itemOffers":    {Rate: 0.5, Burst: 1}, // getItemOffersBatch
	"/batches/products/pricing/v0/listingOffers": {Rate: 0.5, Burst: 1}, // getListingOffersBatch

	// ── Product Pricing API v2022-05-01 ────────────────────────────────
	"/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice": {Rate: 0.033, Burst: 1}, // ~2 req/min
	"/products/pricing/2022-05-01/offer/competitiveSummary":         {Rate: 0.033, Burst: 1}, // ~2 req/min

	// ── Product Fees API v0 ────────────────────────────────────────────
	"/products/fees/v0/listings/{sku}/feesEstimate": {Rate: 1.0, Burst: 2},
	"/products/fees/v0/items/{asin}/feesEstimate":   {Rate: 1.0, Burst: 2},
	"/products/fees/v0/feesEstimate":                {Rate: 0.5, Burst: 1}, // Batch up to 20 items

	// ── Product Type Definitions API v2020-09-01 ───────────────────────
	"/definitions/2020-09-01/productTypes":               {Rate: 5.0, Burst: 10}, // grantless
	"/definitions/2020-09-01/productTypes/{productType}": {Rate: 5.0, Burst: 10}, // grantless

	// ── FBA Inventory API v1 ───────────────────────────────────────────
	"/fba/inventory/v1/summaries":       {Rate: 2.0, Burst: 2},
	"/fba/inventory/v1/items":           {Rate: 2.0, Burst: 3},
	"/fba/inventory/v1/items/{sku}":     {Rate: 2.0, Burst: 3},
	"/fba/inventory/v1/items/inventory": {Rate: 2.0, Burst: 3},

	// ── FBA Inbound Eligibility API v1 ─────────────────────────────────
	"/fba/inbound/v1/eligibility/itemPreview": {Rate: 1.0, Burst: 1},

	// ── Fulfillment Inbound API v2024-03-20 ────────────────────────────
	// Most operations: Rate 2, Burst 2-30. Some elevated to Rate 5 in June 2025.
	"/fba/inbound/v2024-03-20/inboundPlans":                           {Rate: 2.0, Burst: 6},  // listInboundPlans
	"/fba/inbound/v2024-03-20/inboundPlans/{planId}":                  {Rate: 2.0, Burst: 6},  // getInboundPlan
	"/fba/inbound/v2024-03-20/inboundPlans/{planId}/packingOptions":   {Rate: 2.0, Burst: 2},  // generatePackingOptions
	"/fba/inbound/v2024-03-20/inboundPlans/{planId}/placementOptions": {Rate: 2.0, Burst: 2},  // generatePlacementOptions

	// ── Fulfillment Inbound API v0 (Legacy) ────────────────────────────
	"/fba/inbound/v0/prepInstructions":                    {Rate: 2.0, Burst: 30},
	"/fba/inbound/v0/shipments":                           {Rate: 2.0, Burst: 30},
	"/fba/inbound/v0/shipments/{shipmentId}/labels":       {Rate: 5.0, Burst: 30},
	"/fba/inbound/v0/shipments/{shipmentId}/billOfLading": {Rate: 2.0, Burst: 30},
	"/fba/inbound/v0/shipments/{shipmentId}/items":        {Rate: 2.0, Burst: 30},
	"/fba/inbound/v0/shipmentItems":                       {Rate: 2.0, Burst: 30},

	// ── Fulfillment Outbound API v2020-07-01 (Multi-Channel) ───────────
	"/fba/outbound/2020-07-01/fulfillmentOrders":                      {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/fulfillmentOrders/preview":              {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}":            {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}/cancel":     {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/fulfillmentOrders/{orderId}/return":     {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/deliveryOffers":                         {Rate: 10.0, Burst: 30}, // highest rate in fulfillment
	"/fba/outbound/2020-07-01/tracking":                               {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/returnReasonCodes":                      {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/features":                               {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/features/inventory/{featureName}":       {Rate: 2.0, Burst: 30},
	"/fba/outbound/2020-07-01/features/inventory/{featureName}/{sku}": {Rate: 2.0, Burst: 30},

	// ── Shipping API v1 ────────────────────────────────────────────────
	"/shipping/v1/shipments":                             {Rate: 5.0, Burst: 15},
	"/shipping/v1/shipments/{shipmentId}":                {Rate: 5.0, Burst: 15},
	"/shipping/v1/shipments/{shipmentId}/cancel":         {Rate: 5.0, Burst: 15},
	"/shipping/v1/shipments/{shipmentId}/purchaseLabels": {Rate: 5.0, Burst: 15},
	"/shipping/v1/containers/{trackingId}/label":         {Rate: 5.0, Burst: 15},
	"/shipping/v1/purchaseShipment":                      {Rate: 5.0, Burst: 15},
	"/shipping/v1/rates":                                 {Rate: 5.0, Burst: 15},
	"/shipping/v1/account":                               {Rate: 5.0, Burst: 15},
	"/shipping/v1/tracking/{trackingId}":                 {Rate: 1.0, Burst: 1}, // lower rate

	// ── Shipping API v2 ────────────────────────────────────────────────
	// All operations: Rate 5, Burst 15 (default quota).
	"/shipping/v2/shipments":                             {Rate: 5.0, Burst: 15},
	"/shipping/v2/tracking":                              {Rate: 5.0, Burst: 15},
	"/shipping/v2/rates":                                 {Rate: 5.0, Burst: 15},
	"/shipping/v2/shipments/{shipmentId}/cancel":         {Rate: 5.0, Burst: 15},
	"/shipping/v2/shipments/{shipmentId}/documents":      {Rate: 5.0, Burst: 15},
	"/shipping/v2/oneClickShipment":                      {Rate: 5.0, Burst: 15},
	"/shipping/v2/shipments/{shipmentId}/directPurchase": {Rate: 5.0, Burst: 15},
	"/shipping/v2/collectionForms":                       {Rate: 5.0, Burst: 15},

	// ── Merchant Fulfillment API v0 ────────────────────────────────────
	"/mfn/v0/eligibleShippingServices": {Rate: 6.0, Burst: 12},
	"/mfn/v0/shipments":                {Rate: 2.0, Burst: 2},
	"/mfn/v0/shipments/{shipmentId}":   {Rate: 1.0, Burst: 1},
	"/mfn/v0/sellerInputs":             {Rate: 1.0, Burst: 1},

	// ── Easy Ship API v2022-03-23 ──────────────────────────────────────
	// All operations: Rate 1, Burst 1.
	"/easyShip/2022-03-23/timeSlot":      {Rate: 1.0, Burst: 1},
	"/easyShip/2022-03-23/package":       {Rate: 1.0, Burst: 1},
	"/easyShip/2022-03-23/packages/bulk": {Rate: 1.0, Burst: 1},

	// ── Notifications API v1 ───────────────────────────────────────────
	// All operations: Rate 1, Burst 5.
	"/notifications/v1/subscriptions/{type}":         {Rate: 1.0, Burst: 5},
	"/notifications/v1/subscriptions":                {Rate: 1.0, Burst: 5},
	"/notifications/v1/destinations":                 {Rate: 1.0, Burst: 5},
	"/notifications/v1/destinations/{destinationId}": {Rate: 1.0, Burst: 5},

	// ── Tokens API v2021-03-01 ─────────────────────────────────────────
	"/tokens/2021-03-01/restrictedDataToken": {Rate: 1.0, Burst: 10},

	// ── Sellers API v1 ─────────────────────────────────────────────────
	"/sellers/v1/marketplaceParticipations": {Rate: 0.016, Burst: 15},
	"/sellers/v1/account":                   {Rate: 0.016, Burst: 15},

	// ── Sales API v1 ───────────────────────────────────────────────────
	"/sales/v1/orderMetrics": {Rate: 0.5, Burst: 15},

	// ── Finances API v0 ────────────────────────────────────────────────
	// All operations: Rate 0.5, Burst 30.
	"/finances/v0/financialEvents":                                     {Rate: 0.5, Burst: 30},
	"/finances/v0/financialEventGroups":                                {Rate: 0.5, Burst: 30},
	"/finances/v0/financialEventGroups/{eventGroupId}/financialEvents": {Rate: 0.5, Burst: 30},
	"/finances/v0/orders/{orderId}/financialEvents":                    {Rate: 0.5, Burst: 30},

	// ── Finances API v2024-06-19 ───────────────────────────────────────
	"/finances/2024-06-19/transactions": {Rate: 0.5, Burst: 10},

	// ── Messaging API v1 ───────────────────────────────────────────────
	// All operations: Rate 1, Burst 5.
	"/messaging/v1/orders/{orderId}/messages": {Rate: 1.0, Burst: 5},

	// ── Solicitations API v1 ───────────────────────────────────────────
	"/solicitations/v1/orders/{orderId}/solicitations/productReviewAndSellerFeedback": {Rate: 1.0, Burst: 5},
	"/solicitations/v1/orders/{orderId}/solicitations":                                {Rate: 1.0, Burst: 5},

	// ── Uploads API v2020-11-01 ────────────────────────────────────────
	"/uploads/2020-11-01/uploadDestinations/{resource}": {Rate: 0.1, Burst: 5},

	// ── Application Management API v2023-11-30 ─────────────────────────
	"/applications/2023-11-30/clientSecret": {Rate: 0.0167, Burst: 1}, // grantless

	// ── A+ Content Management API v2020-11-01 ──────────────────────────
	// All operations: Rate 10, Burst 10. Available for Seller and Vendor.
	"/aplus/2020-11-01/contentDocuments":                       {Rate: 10.0, Burst: 10},
	"/aplus/2020-11-01/contentDocuments/{contentReferenceKey}": {Rate: 10.0, Burst: 10},
	"/aplus/2020-11-01/contentPublishRecords":                  {Rate: 10.0, Burst: 10},

	// ── Replenishment API v2022-11-07 ──────────────────────────────────
	"/replenishment/2022-11-07/sellingPartners/metrics/search": {Rate: 1.0, Burst: 1},
	"/replenishment/2022-11-07/offers/metrics/search":          {Rate: 1.0, Burst: 1},
	"/replenishment/2022-11-07/offers/search":                  {Rate: 1.0, Burst: 1},

	// ── AWD API v2024-05-09 ────────────────────────────────────────────
	"/awd/2024-05-09/inboundShipments":              {Rate: 1.0, Burst: 1},
	"/awd/2024-05-09/inboundShipments/{shipmentId}": {Rate: 2.0, Burst: 6},
	"/awd/2024-05-09/inventory":                     {Rate: 2.0, Burst: 2},

	// ── Supply Sources API v2020-07-01 ─────────────────────────────────
	// All operations: Rate 1, Burst 10.
	"/supplySources/2020-07-01/supplySources":                  {Rate: 1.0, Burst: 10},
	"/supplySources/2020-07-01/supplySources/{supplySourceId}": {Rate: 1.0, Burst: 10},

	// ── Vendor Direct Fulfillment Orders ───────────────────────────────
	// All Vendor APIs: Rate 10, Burst 10 consistently.
	"/vendor/directFulfillment/orders/2021-12-28/purchaseOrders":                       {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/{purchaseOrderNumber}": {Rate: 10.0, Burst: 10},

	// ── Vendor Direct Fulfillment Shipping ─────────────────────────────
	"/vendor/directFulfillment/shipping/2021-12-28/shippingLabels":                         {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/{purchaseOrderNumber}":   {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/shipmentConfirmations":                  {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/shipmentStatusUpdates":                  {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/customerInvoices":                       {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/{purchaseOrderNumber}": {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/packingSlips":                           {Rate: 10.0, Burst: 10},
	"/vendor/directFulfillment/shipping/2021-12-28/packingSlips/{purchaseOrderNumber}":     {Rate: 10.0, Burst: 10},

	// ── Vendor Direct Fulfillment Inventory ────────────────────────────
	"/vendor/directFulfillment/inventory/2021-12-28/inventoryUpdates": {Rate: 10.0, Burst: 10},

	// ── Vendor Direct Fulfillment Payments ─────────────────────────────
	"/vendor/directFulfillment/payments/2021-12-28/invoices": {Rate: 10.0, Burst: 10},

	// ── Vendor Direct Fulfillment Transactions ─────────────────────────
	"/vendor/directFulfillment/transactions/2021-12-28/transactions/{transactionId}": {Rate: 10.0, Burst: 10},

	// ── Vendor Orders v1 ───────────────────────────────────────────────
	"/vendor/orders/v1/purchaseOrders":                       {Rate: 10.0, Burst: 10},
	"/vendor/orders/v1/purchaseOrders/{purchaseOrderNumber}": {Rate: 10.0, Burst: 10},
	"/vendor/orders/v1/purchaseOrdersStatus":                 {Rate: 10.0, Burst: 10},
	"/vendor/orders/v1/acknowledgements":                     {Rate: 10.0, Burst: 10},

	// ── Vendor Shipments v1 ────────────────────────────────────────────
	"/vendor/shipments/v1/shipmentConfirmations": {Rate: 10.0, Burst: 10},
	"/vendor/shipments/v1/shipments":             {Rate: 10.0, Burst: 10},
	"/vendor/shipments/v1/shipmentLabels":        {Rate: 10.0, Burst: 10},

	// ── Vendor Invoices v1 ─────────────────────────────────────────────
	"/vendor/invoices/v1/invoices": {Rate: 10.0, Burst: 10},

	// ── Vendor Transaction Status v1 ───────────────────────────────────
	"/vendor/transactionStatus/v1/transactions/{transactionId}": {Rate: 10.0, Burst: 10},
}

// MethodBucketOverrides contains per-method overrides where a specific HTTP method
// has different rate/burst values than the default for that endpoint.
// Key format: "METHOD:endpoint"
//
// Note: Per the Amazon reference, Listings Items has uniform Rate 5, Burst 5 for all
// methods at the pair level. The differentiation happens at the app level (see AppBucketParams).
// The Feeds createFeed (POST) has a lower rate than getFeeds (GET).
var MethodBucketOverrides = map[string]BucketParams{
	// Feeds API: createFeed (POST) has lower rate than getFeeds (GET)
	"POST:/feeds/2021-06-30/feeds": {Rate: 0.0083, Burst: 15}, // createFeed ~1 req/2min

	// Reports API: createReport (POST) has different rate than getReports (GET)
	"POST:/reports/2021-06-30/reports": {Rate: 0.0167, Burst: 15}, // createReport ~1 req/min

	// Data Kiosk: createQuery (POST) has different rate than getQueries (GET)
	"POST:/datakiosk/2023-11-15/queries": {Rate: 0.0167, Burst: 15}, // createQuery
}

// AppBucketParams defines application-level (cross-merchant) rate limits.
// These are enforced in addition to per-merchant (pair-level) limits.
// Key format: "METHOD:endpoint"
//
// Source: Amazon SP-API reference  -  Catalog Items, Listings Items, and selected other APIs
// have documented application-wide rate limits.
var AppBucketParams = map[string]BucketParams{
	// ── Catalog Items API ──────────────────────────────────────────────
	// searchCatalogItems: 500 req/s app-wide (keyword searches: 50 req/s  -  not enforced here)
	"GET:/catalog/2022-04-01/items":        {Rate: 500.0, Burst: 500},
	"GET:/catalog/2022-04-01/items/{asin}": {Rate: 250.0, Burst: 250}, // getCatalogItem

	// ── Listings Items API ─────────────────────────────────────────────
	// Four-level throttling. App-level limits below.
	"GET:/listings/2021-08-01/items/{sellerId}/{sku}":    {Rate: 100.0, Burst: 100}, // getListingsItem
	"PUT:/listings/2021-08-01/items/{sellerId}/{sku}":    {Rate: 100.0, Burst: 100}, // putListingsItem
	"PATCH:/listings/2021-08-01/items/{sellerId}/{sku}":  {Rate: 500.0, Burst: 500}, // patchListingsItem (sub-limits: relationships 100/s, product data 100/s, validation 20/s)
	"DELETE:/listings/2021-08-01/items/{sellerId}/{sku}": {Rate: 100.0, Burst: 100}, // deleteListingsItem
	"GET:/listings/2021-08-01/items/{sellerId}":          {Rate: 100.0, Burst: 100}, // searchListingsItems
}

// ClassifyEndpoint normalizes a request path to its endpoint pattern.
func ClassifyEndpoint(path string) string {
	return endpoint.Classify(path)
}

// LookupDefaults returns the default bucket params for a given HTTP method and
// classified endpoint. It first checks for a method-specific override, then
// falls back to the default (method-agnostic) params.
func LookupDefaults(method, endpoint string) (BucketParams, bool) {
	// Check method-specific override first
	key := method + ":" + endpoint
	if params, ok := MethodBucketOverrides[key]; ok {
		return params, true
	}
	// Fall back to default
	params, ok := DefaultBucketParams[endpoint]
	return params, ok
}
