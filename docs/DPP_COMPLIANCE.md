# DPP Compliance

Smart Proxy implements built-in support for Amazon's **Data Protection Policy (DPP)** by detecting and redacting Personally Identifiable Information (PII) before it reaches logs, caches, or persistent storage.

## Overview

Amazon's DPP requires that SP-API developers:
- Only access PII when necessary for order fulfillment
- Not store PII longer than required
- Protect PII from unauthorized access
- Not cache sensitive buyer/shipping data unnecessarily

Smart Proxy enforces these requirements automatically at the proxy layer, so your application benefits from DPP compliance without additional code.

## How It Works

### PII Detection

The proxy maintains a **registry of PII-containing endpoints** and their sensitive fields. Detection happens at two levels:

#### Full-Body PII Endpoints

These endpoints return entirely PII-sensitive data. The entire response body is redacted in logs:

| Endpoint | Content |
|---|---|
| `/orders/v0/orders/{orderId}/buyerInfo` | Buyer email, name |
| `/orders/v0/orders/{orderId}/address` | Shipping address |
| `/orders/v0/orders/{orderId}/orderItems/buyerInfo` | Item-level buyer info |
| `/messaging/v1/orders/{orderId}/messages/{messageId}` | Buyer/seller messages |

#### Partial PII Fields

These endpoints contain PII in specific fields, identified by JSONPath:

| API | Redacted Fields |
|---|---|
| Orders (`/orders/v0/orders`) | BuyerEmail, BuyerName, ShippingAddress, BuyerTaxInfo |
| Order Items (`/orders/v0/orders/{id}/orderItems`) | BuyerCustomizedInfo, GiftMessageText |
| Shipping (`/shipping/v2/shipments`) | ShipTo (name, address, phone, email) |
| MFN Shipments (`/mfn/v0/shipments`) | ShipToAddress, ShipFromAddress |
| FBA Outbound (`/fba/outbound/.../fulfillmentOrders`) | DestinationAddress |
| Messaging (`/messaging/v1/orders/{id}/messages`) | MessageText, Attachments |
| Finances (`/finances/v0/financialEvents`) | OrderId references |
| Easy Ship (`/easyShip/2022-03-23/package`) | OrderId, PickupSlot |

#### Query Parameter Detection

The proxy also detects PII requests via query parameters:

```
/orders/v0/orders?dataElements=buyerInfo,shippingAddress
```

When `dataElements` includes `buyerInfo` or `shippingAddress`, the response is flagged as PII-containing.

## Redaction Modes

Three redaction modes are available for PII fields:

### REDACT (default)

Replaces the field value with `[REDACTED]`:

```json
{
  "BuyerEmail": "[REDACTED]",
  "BuyerName": "[REDACTED]"
}
```

### HASH

Replaces the field value with a deterministic SHA-256 hash:

```json
{
  "BuyerEmail": "sha256:a1b2c3d4e5f6..."
}
```

The same input always produces the same hash, enabling correlation analysis without exposing the raw value.

### OMIT

Removes the field entirely from the JSON output:

```json
{
  // BuyerEmail and BuyerName are not present
  "OrderId": "123-456-789"
}
```

## What Gets Protected

### Logging

All PII is redacted **before** being written to logs:

- **Full-body endpoints:** Entire response body replaced with `{"redacted": true, "endpoint": "..."}`
- **Partial PII:** Specific fields redacted according to the configured mode
- **Headers:** Sensitive headers (`Authorization`, `x-amz-access-token`, `x-amz-security-token`) are always redacted

### Caching

When `SP_PROXY_CACHE_EXCLUDE_PII=true` (the default):

- PII-containing responses are **not stored** in the cache
- The response header `X-SP-Proxy-Cache: PII_EXCLUDED` indicates the exclusion
- This prevents PII from persisting in memory longer than necessary

### Original Response

The **original, unredacted response** is always forwarded to your application unchanged. Redaction only applies to the proxy's internal logging and caching. Your application receives the full data from Amazon as expected.

## DPP Compliance Checklist

| Requirement | How Smart Proxy Helps |
|---|---|
| Minimize PII access | Cache exclusion prevents unnecessary PII storage |
| Protect PII in transit | PII redacted before reaching logs/storage |
| Limit PII retention | Automatic purge jobs with configurable retention periods |
| Audit PII access | All requests logged with PII redaction flags (`meta.PIIRedacted`) |
| Secure PII storage | PII never written to disk in readable form (redacted or hashed) |
| Support data deletion | Purge jobs automatically remove old data; no PII persists beyond retention |

## Header Redaction

The following headers are always redacted in log output, regardless of endpoint:

| Header | Reason |
|---|---|
| `Authorization` | Contains LWA access token |
| `x-amz-access-token` | SP-API access token |
| `x-amz-security-token` | AWS STS session token |

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SP_PROXY_CACHE_EXCLUDE_PII` | `true` | Exclude PII-containing responses from cache |
| `SP_PROXY_PII_FAIL_CLOSED` | `false` | Treat any path that does not match a registered SP-API endpoint as PII (full-body redact, exclude from cache). Recommended for production. |
| `SP_PROXY_PURGE_METADATA_RETENTION` | `720h` (30 days) | How long request logs are retained |
| `SP_PROXY_PURGE_AUDIT_RETENTION` | `8760h` (365 days) | How long audit logs are retained |
| `SP_PROXY_BODIES_RECENT_MAX_AGE` | `72h` (3 days) | Recent body file retention |
| `SP_PROXY_BODIES_ARCHIVE_MAX_AGE` | `8760h` (365 days) | Archived body file retention |

### Fail-closed mode

The PII registry maps known SP-API endpoints to their PII fields. If Amazon ships a new endpoint and it is not yet in the registry, the default behavior is "no rules apply" and the response body is logged and cached as-is. This is the **fail-open** posture: it preserves observability for unknown endpoints at the cost of leaking PII if the new endpoint happens to return any.

Setting `SP_PROXY_PII_FAIL_CLOSED=true` flips this default. Any path that does not match a registered endpoint pattern (see [internal/endpoint/classify.go](../internal/endpoint/classify.go)) is treated as full-body PII: the response body is replaced with a placeholder in logs, and the response is excluded from the cache when `SP_PROXY_CACHE_EXCLUDE_PII=true`. The original response is still forwarded to your application unchanged.

Trade-offs:

- **Pro:** new SP-API endpoints cannot silently bypass redaction until you upgrade.
- **Con:** dashboard log detail views show `{"redacted": true, ...}` for any endpoint not yet in the registry. You will need to update [internal/pii/registry.go](../internal/pii/registry.go) when Amazon adds endpoints you actually use.

Enable this in production once you have visibility into your traffic shape and have confirmed all in-use endpoints are mapped.

## Best Practices

1. **Keep `CACHE_EXCLUDE_PII=true`**  -  This is the default and ensures PII is never cached
2. **Set appropriate retention periods**  -  Shorter retention = less PII exposure risk
3. **Use HASH mode for analytics**  -  If you need to correlate buyer activity without exposing PII
4. **Use OMIT mode for maximum safety**  -  When PII fields are not needed in logs at all
5. **Monitor request logs**  -  The `PIIRedacted` flag in request logs shows which requests contained PII
