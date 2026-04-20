# Auto-RDT (Restricted Data Token) Handling

Smart Proxy can automatically manage Restricted Data Tokens for SP-API endpoints that require them. When enabled, the proxy detects PII-restricted requests, mints RDTs on-the-fly, and caches them for reuse across subsequent calls.

## Overview

- **Opt-in via environment variable:** `SP_PROXY_RDT_AUTO_MINT=true`
- **Covers all restricted SP-API operations:** Orders v0, MFN, Shipping v1, Direct Fulfillment, Easy Ship, Shipment Invoicing (Brazil), and restricted Reports
- **Automatic caching:** One RDT mint covers all requests to the same operation class for 55 minutes
- **Fail-open:** If minting fails, the original request is forwarded unchanged
- **Per-request override:** `X-SP-Proxy-Force-RDT` header for explicit control

## How It Works

### The Problem

Many SP-API endpoints that return personally identifiable information (PII) require a Restricted Data Token instead of a regular LWA access token. Without the proxy, every client must:

1. Detect which endpoints need RDTs
2. Call `createRestrictedDataToken` with the right path and dataElements
3. Use the returned RDT instead of the LWA token
4. Handle token expiry and caching

This is tedious, error-prone, and leads to redundant RDT minting calls.

### The Solution

With `SP_PROXY_RDT_AUTO_MINT=true`, the proxy handles all of this transparently:

```
Client sends:
  GET /orders/v0/orders/123-456
  x-amz-access-token: Atza|<lwa-token>

Proxy detects: PII endpoint, LWA token (not RDT)
Proxy checks cache: miss
Proxy mints RDT using client's LWA token
Proxy caches RDT for 55 minutes
Proxy swaps token and forwards:
  GET /orders/v0/orders/123-456
  x-amz-access-token: Atz.sprdt|<rdt-token>

Next request to any /orders/v0/orders/{orderId}:
  Cache hit - no new mint needed
```

### Request Flow

```
1. Is Auto-RDT enabled?                     --> No: pass through
2. X-SP-Proxy-Force-RDT: false?             --> pass through (explicit opt-out)
3. X-SP-Proxy-Force-RDT: true?              --> mint with concrete path (explicit opt-in)
4. Is this a report-related path?            --> handle via report sniffing (see below)
5. Does path match a restricted operation?   --> No: pass through
6. Is token already an RDT (Atz.sprdt|)?     --> Yes: pass through
7. Cache lookup for (merchant, operation, dataElements)
   |
   +-- HIT: Swap token, forward
   |
   +-- MISS: Mint RDT via Tokens API (deduplicated via singleflight)
             Cache the new RDT
             Swap token, forward
8. If upstream returns 403 after swap:       --> Invalidate cache entry
```

## Generic Path Caching

This is the key insight that makes Auto-RDT highly effective: Amazon's Tokens API accepts **generic path forms** with literal placeholder strings like `/orders/v0/orders/{orderId}`. An RDT minted with a generic path works for **all resources matching that pattern** for the minting seller.

This means:
- One RDT for `/orders/v0/orders/{orderId}` covers GET requests to any specific order
- One RDT for `/mfn/v0/shipments/{shipmentId}` covers GET requests to any MFN shipment
- Cache hit rate after the first mint approaches 100% within the 55-minute window

### Cache Key

```
(merchant_id, generic_path, sorted_data_elements)
```

Example: `("SELLER_XYZ", "/orders/v0/orders/{orderId}", "buyerInfo,buyerTaxInformation,shippingAddress")`

### Cache Lifetime

RDTs are valid for 60 minutes (as reported by Amazon in the `expiresIn` response field). The proxy applies a 5-minute safety margin, treating cached RDTs as expired after 55 minutes. The TTL is always read from the API response, never hardcoded.

## Covered Endpoints

### Orders v0 (with dataElements)

These endpoints require all three dataElements (`buyerInfo`, `shippingAddress`, `buyerTaxInformation`). The proxy always sends all three to avoid silent empty PII fields.

| Operation | Generic Path |
|---|---|
| getOrders (list) | `/orders/v0/orders` |
| getOrder | `/orders/v0/orders/{orderId}` |
| getOrderItems | `/orders/v0/orders/{orderId}/orderItems` |

### Orders v0 (without dataElements)

| Operation | Generic Path |
|---|---|
| getOrderAddress | `/orders/v0/orders/{orderId}/address` |
| getOrderBuyerInfo | `/orders/v0/orders/{orderId}/buyerInfo` |
| getOrderItemsBuyerInfo | `/orders/v0/orders/{orderId}/orderItems/buyerInfo` |
| getOrderRegulatedInfo | `/orders/v0/orders/{orderId}/regulatedInfo` |

### Merchant Fulfillment (MFN) v0

| Operation | Generic Path | Method |
|---|---|---|
| getShipment | `/mfn/v0/shipments/{shipmentId}` | GET |
| cancelShipment | `/mfn/v0/shipments/{shipmentId}/cancel` | PUT |
| createShipment | `/mfn/v0/shipments` | POST |

### Shipping v1

| Operation | Generic Path |
|---|---|
| getShipment | `/shipping/v1/shipments/{shipmentId}` |

### Shipment Invoicing (Brazil)

| Operation | Generic Path |
|---|---|
| getShipmentDetails | `/fba/outbound/brazil/v0/shipments/{shipmentId}` |

### Easy Ship

| Operation | Generic Path | Method |
|---|---|---|
| createScheduledPackageBulk | `/easyShip/2022-03-23/packages/bulk` | POST |

### Direct Fulfillment Orders

| Operation | Generic Path |
|---|---|
| getOrders (list) | `/vendor/directFulfillment/orders/2021-12-28/purchaseOrders` |
| getOrder | `/vendor/directFulfillment/orders/2021-12-28/purchaseOrders/{purchaseOrderNumber}` |

### Direct Fulfillment Shipping

| Operation | Generic Path |
|---|---|
| getShippingLabels (list) | `/vendor/directFulfillment/shipping/2021-12-28/shippingLabels` |
| getShippingLabel | `/vendor/directFulfillment/shipping/2021-12-28/shippingLabels/{purchaseOrderNumber}` |
| getPackingSlips (list) | `/vendor/directFulfillment/shipping/2021-12-28/packingSlips` |
| getPackingSlip | `/vendor/directFulfillment/shipping/2021-12-28/packingSlips/{purchaseOrderNumber}` |
| getCustomerInvoices (list) | `/vendor/directFulfillment/shipping/2021-12-28/customerInvoices` |
| getCustomerInvoice | `/vendor/directFulfillment/shipping/2021-12-28/customerInvoices/{purchaseOrderNumber}` |

## Report Documents

Report documents are handled differently from other restricted endpoints because Amazon **requires the concrete `reportDocumentId` in the RDT path**. Generic placeholders are rejected. This means RDTs for report documents cannot be reused across different documents.

### Which Reports Are Restricted

Only 16 `reportType` values require an RDT for document download:

| Category | reportTypes |
|---|---|
| Order reports (shipping/invoicing/tax) | `GET_FLAT_FILE_ORDER_REPORT_DATA_SHIPPING`, `_INVOICING`, `_TAX`, `GET_ORDER_REPORT_DATA_SHIPPING`, `_INVOICING`, `_TAX`, `GET_FLAT_FILE_ACTIONABLE_ORDER_DATA_SHIPPING`, `_INVOICING`, `_TAX`, `GET_CONVERGED_FLAT_FILE_ORDER_REPORT_DATA` |
| FBA shipments | `GET_AMAZON_FULFILLED_SHIPMENTS_DATA_INVOICING`, `_TAX` |
| Tax and invoicing | `GET_EASYSHIP_DOCUMENTS`, `GET_GST_MTR_B2B_CUSTOM`, `GET_VAT_TRANSACTION_DATA`, `SC_VAT_TAX_REPORT` |

All other report types (inventory, listings, sales, etc.) do not require RDTs.

### Report Sniffing Flow

The proxy tracks the 3-step report lifecycle to determine which document downloads need RDTs:

```
Step 1: POST /reports/2021-06-30/reports
        Proxy sniffs reportType from request body, reportId from response
        --> tracks: reportId -> reportType

Step 2: GET /reports/2021-06-30/reports/{reportId}
        Proxy sniffs reportDocumentId from response body
        --> tracks: reportDocumentId -> reportType

Step 3: GET /reports/2021-06-30/documents/{documentId}
        Proxy looks up reportType for this documentId
        --> restricted type: mint RDT with concrete path, swap token
        --> non-restricted type: pass through unchanged
        --> unknown documentId: pass through unchanged
```

### Reports from Notifications

If your application receives a `REPORT_PROCESSING_FINISHED` notification containing the `reportDocumentId`, the proxy has not seen Steps 1 and 2. In this case, the proxy does not know whether the report is restricted.

Use the `X-SP-Proxy-Force-RDT: true` header to force RDT minting:

```bash
curl http://localhost:8080/reports/2021-06-30/documents/amzn1.spdoc.1.4.na.abc123 \
  -H "x-amz-access-token: Atza|..." \
  -H "X-SP-Proxy-Force-RDT: true"
```

## Per-Request Override: X-SP-Proxy-Force-RDT

This header gives explicit control over RDT handling for any individual request, overriding all automatic detection logic.

| Value | Behavior |
|---|---|
| `true` | Force-mint an RDT using the concrete request path. Works on any endpoint, even those not in the restricted operations table. |
| `false` | Skip all RDT handling. The original token is forwarded to Amazon unchanged, even if the endpoint would normally trigger auto-minting. |
| (not set) | Normal auto-detection flow. |

The header is **always stripped** before the request is forwarded to Amazon.

### Use Cases

**Force-mint for report documents from notifications:**
```bash
# Client got reportDocumentId from REPORT_PROCESSING_FINISHED notification
curl http://localhost:8080/reports/2021-06-30/documents/amzn1.spdoc.1.4.na.abc123 \
  -H "x-amz-access-token: Atza|..." \
  -H "X-SP-Proxy-Force-RDT: true"
```

**Opt out for non-PII order calls:**
```bash
# Application only needs order status, not PII fields
curl http://localhost:8080/orders/v0/orders/123-456 \
  -H "x-amz-access-token: Atza|..." \
  -H "X-SP-Proxy-Force-RDT: false"
```

## Singleflight Deduplication

When multiple concurrent requests target the same restricted operation for the same merchant, only one `createRestrictedDataToken` call is made upstream. All concurrent requests share the result. This prevents thundering-herd scenarios where parallel order fetches would each trigger their own RDT mint.

Example: 10 concurrent `GET /orders/v0/orders/{different-order-ids}` requests from the same seller result in exactly 1 Tokens API call.

## Error Handling

The proxy follows a **fail-open** strategy:

| Scenario | Behavior |
|---|---|
| RDT minting fails (network error, 500, etc.) | Forward original request with LWA token unchanged |
| Upstream returns 403 after token swap | Invalidate cache entry, forward the 403 to client |
| Token is already an RDT (`Atz.sprdt\|` prefix) | Pass through unchanged, no minting |
| Feature disabled (`SP_PROXY_RDT_AUTO_MINT=false`) | All requests pass through unchanged |

The proxy never makes things worse than they would be without it. If something goes wrong with RDT handling, the client sees the same response it would have received talking directly to Amazon.

## Logging

When the proxy mints an RDT itself (cache miss), the `createRestrictedDataToken` call flows through the same logging pipeline as any other request. It appears in:

- Request logs (with the Tokens API path)
- Prometheus metrics

This gives full visibility into how many RDTs the proxy mints and for which operations.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SP_PROXY_RDT_AUTO_MINT` | `false` | Enable automatic RDT minting for restricted endpoints |

No other configuration is needed. The RDT cache uses a fixed 5-minute safety margin and in-memory storage. Cache entries are lost on proxy restart, which is acceptable since clients simply trigger a new mint on the next request.

## Orders v0 Sunset (March 2027)

Orders v0 is the largest surface area for Auto-RDT. Amazon has announced its sunset for **March 27, 2027**. The replacement, Orders API v2026-01-01, uses role-based PII access and does not require RDTs.

After the sunset, Auto-RDT remains useful for:
- Merchant Fulfillment (MFN) v0
- Shipping v1
- Shipment Invoicing (Brazil)
- Easy Ship
- Direct Fulfillment Orders and Shipping
- Restricted Report documents

## Architecture

### Middleware Position

```
prommetrics -> merchant -> logging -> RDT -> cache -> ratelimit -> proxy
```

The RDT middleware runs after merchant resolution (needs merchant ID for cache keys) and after logging (RDT mint calls are logged), but before the response cache and rate limiter.

### Package Structure

```
internal/rdt/
  matcher.go          -- PII path matching table (23 restricted operations)
  matcher_test.go     -- 37 table-driven tests
  cache.go            -- In-memory RDT token cache with TTL + safety margin
  cache_test.go       -- 10 tests
  mint.go             -- Upstream createRestrictedDataToken calls
  mint_test.go        -- 6 tests
  reports.go          -- Report tracker (reportId -> reportType -> documentId correlation)
  reports_test.go     -- 28 tests (tracker + restricted report type list)
  middleware.go       -- HTTP middleware tying it all together
  middleware_test.go  -- 17 tests (full flow, singleflight, force header, report sniffing)
```

### RDT Cache vs. Response Cache

The proxy has two separate caches that serve different purposes:

| | Response Cache (`internal/cache`) | RDT Cache (`internal/rdt`) |
|---|---|---|
| **What it stores** | Full HTTP responses (body + headers) | RDT tokens (short strings) |
| **Key** | merchant + method + path + query | merchant + generic path + dataElements |
| **TTL** | Varies by endpoint tier (30s to 24h) | 55 minutes (fixed, from Amazon's TTL) |
| **Scope** | Per exact request URL | Per operation class (generic path) |
| **Memory** | Configurable (default 256 MB) | Negligible (tokens are small) |
| **Persistence** | In-memory only | In-memory only |

They operate independently. A response cache miss does not imply an RDT cache miss, and vice versa.
