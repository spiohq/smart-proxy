# Caching

Smart Proxy includes an intelligent in-memory caching layer designed to minimize redundant API calls to Amazon's SP-API. Effective caching reduces unnecessary requests and improves response times.

## Overview

- **Type:** In-memory LRU (Least Recently Used) with TTL-based expiration
- **Default memory limit:** 256 MB (configurable)
- **PII-aware:** Responses containing PII are excluded from cache by default
- **Mutation-aware:** PUT/DELETE/PATCH requests automatically invalidate related cached entries
- **POST-aware:** Read-only POST endpoints (batch queries, fee estimates, shipping rates) are cached using body-hash or per-element strategies

## Cache Tiers

Every SP-API endpoint is classified into one of four caching tiers based on how frequently the data changes:

### Tier 1: Aggressive (6-24 hours)

Stable reference data that rarely changes.

| API | TTL | Rationale |
|---|---|---|
| Catalog Items (definitions) | 24h | Schema/attribute definitions are static |
| Product Type Definitions | 24h | Product type schemas rarely change |
| Sellers API | 12h | Seller info (marketplace participations) is stable |
| Product Fees (single + batch) | 12h | Amazon's fee schedules change infrequently |
| MFN Additional Seller Inputs | 24h | Required seller input fields are stable |

### Tier 2: Moderate (5 min - 6 hours)

Business data that changes periodically but not in real-time.

| API | TTL | Rationale |
|---|---|---|
| Reports (list, status) | 30min | Report generation is async; polling every few minutes is sufficient |
| Feeds (list, status) | 15min | Feed processing takes time |
| FBA Inventory | 15min | Inventory snapshots update periodically |
| Listings Items | 10min | Listing changes propagate slowly |
| Sales / Finances | 30min | Financial data aggregated over time |
| Notifications (subscriptions) | 10min | Subscription configs change infrequently |
| Product Pricing Batches | 5min | Pricing data is volatile but cacheable short-term |
| Shipping Rates (v1 + v2) | 4h | Carrier rates are stable within hours |
| Eligible Shipping Services | 6h | Service eligibility changes slowly |
| FBA Fulfillment Preview | 2h | Preview results stable within hours |
| EasyShip Time Slots | 1h | Slots are time-sensitive but stable short-term |
| Replenishment Metrics/Offers | 6h | Aggregated data reported in intervals |

### Tier 3: Short (30-120 seconds)

Volatile data or time-sensitive resources.

| API | TTL | Rationale |
|---|---|---|
| Orders | 60s | Orders flow in continuously |
| Product Pricing (single GET) | 60s | Prices can change frequently |
| Pre-signed document URLs | 4min | URLs expire; short cache avoids serving expired links |

### Tier 4: Never Cached

Endpoints that must never be cached as HTTP responses.

| Category | Reason |
|---|---|
| Token / Authorization endpoints | Security-critical; caching HTTP responses would be dangerous |
| Upload endpoints | Each upload is unique |
| Mutations (PUT, DELETE, PATCH) | Must always reach Amazon |
| Non-cacheable POST endpoints | Requests that create or modify resources (e.g., createShipment) |

> **Note:** The Tokens API (`/tokens/2021-03-01/restrictedDataToken`) is never cached as an HTTP response. However, when [Auto-RDT](RDT.md) is enabled, the proxy maintains a **separate in-memory RDT token cache** that stores the minted tokens (not the full HTTP responses) for reuse across requests. These are two independent caches with different semantics. See [docs/RDT.md](RDT.md) for details.

## How It Works

### GET Request Flow

```
1. Incoming GET request
2. Is caching enabled?                    --> No: pass through
3. Does this endpoint contain PII?        --> Yes + ExcludePII=true: pass through (PII_EXCLUDED)
4. Is X-SP-Proxy-No-Cache: true?          --> Yes: pass through (BYPASS)
5. Generate cache key (merchant + method + path + query)
6. Look up in cache
   |
   +-- HIT: Return cached response
   |         Set header: X-SP-Proxy-Cache: HIT
   |
   +-- MISS: Forward to Amazon
             If response is 2xx: store in cache
             Set header: X-SP-Proxy-Cache: MISS
```

### POST Caching Flow

Smart Proxy caches two types of POST endpoints: **batch endpoints** (arrays of items) and **single-item query endpoints** (individual lookups). Both types are read-only queries that happen to use POST due to their request body structure.

```
1. Incoming POST request
2. Is this a batch-cacheable endpoint?    --> Yes: use per-element caching (see below)
3. Is this a POST-cacheable endpoint?     --> Yes: use body-hash caching (see below)
4. Otherwise                              --> Pass through (trigger mutation invalidation)
```

#### Per-Element Batch Caching

Batch endpoints like `getItemOffersBatch` accept arrays of items. Instead of caching the entire batch as one unit, Smart Proxy caches **each element individually**. This enables three important behaviors:

1. **Cross-batch hits.** If you request `[A, B, C]` in one batch and `[A, D]` in another, element `A` is served from cache in the second call.
2. **Order independence.** `[A, B, C]` and `[C, A, B]` produce the same cache result because the per-element keys are independent of array order.
3. **Full-hit assembly.** When ALL elements in a request are cached, the proxy assembles a complete response from cached elements and returns it without calling Amazon. The assembled response preserves the correct wrapper format (`{"responses": [...]}` for pricing endpoints, bare `[...]` for fees).

When even one element is not cached, the **entire request is forwarded upstream**. The proxy does not mix cached and fresh data within a single response. After a successful upstream response, each element is cached individually for future requests.

**Supported batch endpoints:**

| Endpoint | Key per element | TTL |
|---|---|---|
| `POST /batches/products/pricing/v0/itemOffers` | ASIN + MarketplaceId + ItemCondition + CustomerType | 5 min |
| `POST /batches/products/pricing/v0/listingOffers` | SKU + MarketplaceId + ItemCondition + CustomerType | 5 min |
| `POST /batches/products/pricing/2022-05-01/items/competitiveSummary` | ASIN + MarketplaceId + includedData | 5 min |
| `POST /batches/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice` | SKU + MarketplaceId | 5 min |
| `POST /products/fees/v0/feesEstimate` | IdType + IdValue + pricing params | 12 h |

#### Body-Hash POST Caching

Single-item POST endpoints (fee estimates, shipping rates, fulfillment previews) are cached using a **SHA-256 hash of the normalized request body** combined with the URL path as the cache key.

Before hashing, non-identity fields are stripped from the body. These are caller-supplied tracking values that do not affect the response content:

| Stripped field | Used by |
|---|---|
| `Identifier` | Fee estimate endpoints (caller correlation ID) |
| `clientReferenceDetails` | Shipping v2 (caller tracking reference) |

This means two requests with identical parameters but different tracking IDs produce the same cache key and share cached results.

**Supported endpoints:**

| Endpoint | TTL | Rationale |
|---|---|---|
| `POST /products/fees/v0/items/{asin}/feesEstimate` | 12h | Fee schedules are stable |
| `POST /products/fees/v0/listings/{sku}/feesEstimate` | 12h | Fee schedules are stable |
| `POST /shipping/v2/shipments/rates` | 4h | Carrier rates change daily, not per-minute |
| `POST /shipping/v1/rates` | 4h | Same as v2 |
| `POST /mfn/v0/eligibleShippingServices` | 6h | Service eligibility is stable |
| `POST /mfn/v0/additionalSellerInputs` | 24h | Required fields rarely change |
| `POST /fba/outbound/2020-07-01/fulfillmentOrders/preview` | 2h | Preview results stable short-term |
| `POST /easyShip/2022-03-23/timeSlot` | 1h | Time slots are somewhat time-sensitive |
| `POST /replenishment/2022-11-07/sellingPartners/metrics/search` | 6h | Aggregated metrics data |
| `POST /replenishment/2022-11-07/offers/metrics/search` | 6h | Aggregated metrics data |
| `POST /replenishment/2022-11-07/offers/search` | 6h | Offer data changes slowly |

### Cache Key

**GET requests:**
- **Merchant key**  -  isolates cache per seller account
- **HTTP method**  -  always GET
- **Request path**  -  the SP-API endpoint path
- **Query parameters**  -  sorted and normalized
- **Custom key** (optional)  -  via `X-SP-Proxy-Cache-Key` header

**Batch POST requests:**
- Format: `merchantKey:BATCH:path:elementKeySuffix`
- Each element has its own key derived from its identity fields (ASIN/SKU + marketplace + condition etc.)

**Single POST requests:**
- Format: `merchantKey:POST:path:sha256(normalizedBody)`
- Body is parsed as JSON, non-identity fields stripped, then re-serialized and hashed

### TTL Resolution

TTL is determined in priority order:

1. **`X-SP-Proxy-Cache-Until`** header (absolute RFC 3339 timestamp)
2. **`X-SP-Proxy-Cache-TTL`** header (duration string, e.g., `10m`)
3. **Tier default TTL** (based on endpoint classification)
4. **Global default** (`SP_PROXY_CACHE_DEFAULT_TTL`, default `60s`)

This applies to all cache types (GET, batch POST, and single POST).

### Cache Invalidation

When a mutation request (PUT, DELETE, PATCH) passes through the proxy, it triggers **prefix-based invalidation**:

```
DELETE /listings/2021-08-01/items/SELLER/SKU123
  --> Invalidates all cached GET entries matching:
      /listings/2021-08-01/items/SELLER/*
```

This ensures that after you update a listing, the next GET request fetches fresh data from Amazon.

#### Batch Element Invalidation

When a listing is mutated via PUT or PATCH, the proxy also invalidates **batch-cached listing offer elements** for the affected SKU:

```
PUT /listings/2021-08-01/items/SELLER/MY-SKU
  --> Invalidates cached GET entries (prefix-based, as above)
  --> Also invalidates batch-cached listingOffers elements for MY-SKU
```

This means that after updating a listing, a subsequent `getListingOffersBatch` call that includes that SKU will fetch fresh data from Amazon instead of returning stale cached results.

## Eviction Strategy

The cache uses multiple eviction strategies to stay within memory limits:

1. **TTL expiration**  -  A background sweep runs every 30 seconds to remove expired entries
2. **LRU eviction**  -  When inserting a new entry would exceed `MaxMemory`, the least recently used entries are evicted first
3. **Threshold eviction**  -  When memory usage exceeds 80% of `MaxMemory`, proactive LRU eviction begins

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SP_PROXY_CACHE_ENABLED` | `true` | Enable/disable caching |
| `SP_PROXY_CACHE_MAX_MEMORY` | `268435456` (256 MB) | Maximum memory for cached responses |
| `SP_PROXY_CACHE_DEFAULT_TTL` | `60s` | Fallback TTL when no tier or header applies |
| `SP_PROXY_CACHE_EXCLUDE_PII` | `true` | Skip caching for PII-containing responses |

## Efficiency Gains

Caching reduces the number of requests hitting Amazon's endpoints:

- **Catalog/Definitions** (Tier 1): Cached for 24h  -  a single call per day instead of hundreds
- **Reports/Inventory** (Tier 2): Cached for 15-30min  -  eliminates polling overhead
- **Orders/Pricing** (Tier 3): Cached for 60s  -  prevents duplicate calls from parallel processes
- **Batch pricing** (per-element): Individual ASINs/SKUs are cached across batches, so overlapping batch requests share cache entries
- **Fee estimates** (12h): Amazon's fee structure rarely changes intra-day; a single call per ASIN covers an entire business day
- **Shipping rates** (4h): Carrier rates are stable enough to cache for hours, saving quota on repeated rate lookups

Cache status is visible in the request logs (`HIT`, `MISS`, `PII_EXCLUDED`) so you can monitor exactly how many API calls the proxy saves.
