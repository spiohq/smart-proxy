# Rate Limiting

Smart Proxy implements a per-endpoint token-bucket rate limiter that prevents your application from exceeding Amazon's published rate limits. Instead of receiving 429 errors from Amazon, requests are queued or rejected at the proxy level  -  giving you full control over throttling behavior.

## Overview

- **Algorithm:** Token Bucket (per endpoint, per merchant)
- **Coverage:** 240+ SP-API endpoints with pre-configured limits
- **Dynamic updates:** Reads `x-amzn-RateLimit-Limit` from Amazon responses to adjust bucket rates
- **Throttle safety margin:** Configurable factor (default 0.8) to stay safely below published limits
- **Three modes:** Queue, Reject, Queue-with-Timeout

## How the Token Bucket Works

Each endpoint gets its own token bucket with two parameters:

- **Rate**  -  tokens added per second (matches Amazon's published rate)
- **Burst**  -  maximum tokens the bucket can hold (allows short bursts)

```
Effective rate = published rate x throttle factor

Example: Orders API
  Published rate: 0.0167/s (1 request per minute)
  Throttle factor: 0.8
  Effective rate: 0.01336/s
  Burst: 20 tokens
```

When a request arrives:
1. Check if the bucket has >= 1 token
2. **Yes:** Consume 1 token, forward request immediately
3. **No:** Handle according to the configured mode (queue, reject, or timeout)

Tokens refill continuously at the effective rate. The burst parameter allows brief spikes above the sustained rate.

## Throttle Modes

### Queue (default)

Requests wait in a FIFO queue until tokens become available. The request is forwarded as soon as a token is available.

Best for: Background jobs, bulk operations, non-interactive workloads.

### Reject

Returns `429 Too Many Requests` immediately with a `Retry-After` header indicating when to retry.

Best for: Real-time APIs where stale/delayed responses are worse than errors.

### Queue-Timeout

Requests wait in a queue up to a configurable timeout. If no token becomes available within the timeout, returns `429`.

Best for: Interactive applications with a tolerance for short delays but a hard deadline.

## Mode Resolution

The throttle mode is resolved with the following priority (highest first):

1. **Request header:** `X-SP-Proxy-Throttle-Mode: reject`
2. **Per-merchant config:** `SP_PROXY_RATELIMIT_MERCHANT_MODES`
3. **Per-endpoint config:** `SP_PROXY_RATELIMIT_ENDPOINT_MODES`
4. **Global default:** `SP_PROXY_RATELIMIT_MODE` (default: `queue`)

## Priority Levels

When multiple requests are queued, priority determines dequeue order:

| Priority | Header Value | Use Case |
|---|---|---|
| High | `X-SP-Proxy-Priority: high` | Time-sensitive operations |
| Normal | `X-SP-Proxy-Priority: normal` (default) | Standard requests |
| Low | `X-SP-Proxy-Priority: low` | Background/bulk operations |

## Default Rate Limits

Smart Proxy ships with pre-configured rate limits for 240+ endpoints. Examples:

| API | Rate (req/s) | Burst | Notes |
|---|---|---|---|
| Orders (list) | 0.0167 | 20 | ~1 request/minute |
| Orders (get details) | 0.5 | 30 | |
| Catalog Items (search) | 2.0 | 2 | |
| Listings Items | 5.0 | 10 | |
| Product Pricing | 0.5 | 1 | |
| Feeds (submit) | 0.0083 | 15 | ~1 request/2 minutes |
| Reports (create) | 0.0167 | 15 | |
| Sellers (participations) | 0.016 | 15 | |

These defaults are used when Amazon's response does not include a `x-amzn-RateLimit-Limit` header.

## Dynamic Rate Updates

When Amazon returns a `x-amzn-RateLimit-Limit` header in a response, the proxy automatically updates the bucket's rate to match. This keeps the proxy in sync with any rate-limit changes Amazon makes without requiring a proxy update.

## Response Headers

The proxy adds informational headers to every response:

| Header | Example | Description |
|---|---|---|
| `X-SP-Proxy-Rate-Limit-Active` | `true` | Rate limiting is active for this endpoint |
| `X-SP-Proxy-Rate-Limit-Remaining` | `4.5` | Tokens remaining in the bucket |
| `X-SP-Proxy-Queued` | `true` | Request was queued before forwarding |
| `X-SP-Proxy-Queue-Wait-Ms` | `1200` | Milliseconds spent waiting in queue |
| `Retry-After` | `3` | Seconds until retry (only on 429 responses) |

## Bucket Lifecycle

- **Lazy creation:** Buckets are created on the first request to an endpoint
- **Garbage collection:** Unused buckets are cleaned up after the configured TTL (default 2h)
- **Per-merchant isolation:** Each merchant/seller account gets its own set of buckets

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SP_PROXY_RATELIMIT_ENABLED` | `true` | Enable/disable rate limiting |
| `SP_PROXY_RATELIMIT_MODE` | `queue` | Default mode: `queue`, `reject`, `queue-timeout` |
| `SP_PROXY_RATELIMIT_QUEUE_TIMEOUT` | `60s` | Timeout for `queue-timeout` mode |
| `SP_PROXY_RATELIMIT_QUEUE_MAX_DEPTH` | `100` | Max queued requests per endpoint |
| `SP_PROXY_RATELIMIT_THROTTLE_FACTOR` | `0.8` | Safety margin applied to published limits (0.0-1.0) |
| `SP_PROXY_BUCKET_TTL` | `2h` | TTL for idle rate-limit buckets |

## Cost Impact

Rate limiting prevents wasted API calls from exceeding Amazon's limits:

- **No more 429 retries**  -  Requests are queued instead of rejected by Amazon, eliminating wasted retry calls
- **Throttle factor**  -  Running at 80% of the limit provides a safety buffer and avoids burst-related rejections
- **Combined with caching**  -  Cached responses never consume rate-limit tokens, maximizing effective throughput
- **POST caching amplifies savings**  -  Batch pricing endpoints (0.033-0.5 req/s limit) and fee estimates are cached per-element, so overlapping batches avoid consuming quota entirely
