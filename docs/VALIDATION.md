# Input Validation

Smart Proxy can validate incoming SP-API requests against the official OpenAPI specs before forwarding them to Amazon. Invalid requests are rejected with a `400` response in SP-API's native error format -- no upstream call is made, and no rate-limit token is spent.

---

## Overview

Validation is opt-in and disabled by default (`SP_PROXY_VALIDATION_ENABLED=false`). When enabled:

1. The proxy downloads the full SP-API model ZIP from GitHub on startup and extracts the `models/` subtree.
2. A single composite router is built from all extracted JSON specs.
3. Every incoming request is matched against the router. If a spec match is found, the request is validated (method, path, query parameters, required headers). If no match is found the request passes through unchanged.
4. A background job refreshes the specs on a configurable interval (default 24h) and hot-swaps the router atomically -- no restart required, no in-flight requests are interrupted.

---

## Configuration

| Variable | Default | Description |
|---|---|---|
| `SP_PROXY_VALIDATION_ENABLED` | `false` | Enable proxy-side OpenAPI request validation |
| `SP_PROXY_VALIDATION_SPECS_URL` | `https://github.com/amzn/selling-partner-api-models/archive/refs/heads/main.zip` | URL of the SP-API models ZIP archive |
| `SP_PROXY_VALIDATION_SPECS_DIR` | auto (next to binary) | Local directory for the cached spec files |
| `SP_PROXY_VALIDATION_REFRESH_INTERVAL` | `24h` | Spec refresh interval (any Go duration string, e.g. `12h`, `6h`) |

---

## Error Format

Validation rejections are returned with HTTP status `400` and the standard SP-API error envelope:

```json
{
  "errors": [
    {
      "code": "InvalidInput",
      "message": "parameter 'MarketplaceIds' in query has an error: ...",
      "details": "validated by proxy against SP-API OpenAPI spec"
    }
  ]
}
```

Multiple validation errors within a single request are collected and returned in one response, so the caller sees all problems at once rather than fixing them one by one.

The response also sets `X-SP-Proxy-Validation: rejected`, making it easy to distinguish proxy-generated 400s from Amazon-generated ones in logs and dashboards.

---

## Graceful Degradation

The validator never blocks a request it cannot match:

- **Unknown endpoints:** If the path and method combination is not present in any loaded spec, the request passes through unchanged. This covers private-beta endpoints, newly released operations that haven't appeared in the public models yet, and any internal paths you expose on the same port.
- **Spec download failure on startup:** If the initial download fails and a previously cached set of specs exists on disk, those are used as a fallback. If no cached specs are present either, the middleware becomes a pass-through until the next successful refresh.
- **Extra query parameters:** Parameters not defined in the spec are not flagged. SP-API silently ignores unknown parameters, and so does the validator.

---

## Per-Request Bypass

To skip validation for a single request, send:

```
X-SP-Proxy-Skip-Validation: true
```

The value must be exactly `true` (case-sensitive). Any other value, including `1` or `True`, is ignored and validation proceeds normally.

**Security note:** Because Smart Proxy has no built-in authentication, any reachable client can set this header. See [SECURITY.md](../SECURITY.md#validation-bypass-header) for guidance on restricting this to trusted callers.

---

## What is and is not validated

| Checked | Not checked |
|---|---|
| HTTP method matches spec | Request body content/schema |
| Path parameters present and correctly typed | Response body |
| Required query parameters present | Amazon-specific auth headers (`Authorization`, `x-amz-access-token`) |
| Query parameter types (string, integer, enum values) | Endpoints not present in the SP-API models repo |

Body validation is intentionally excluded. SP-API data feeds and reports transfer payloads via S3 presigned URLs, not through the API itself, so validating request bodies would add latency without meaningful benefit for real-world traffic patterns.

---

## Spec Refresh

On the configured interval, a background scheduler job:

1. Downloads the models ZIP.
2. Extracts it to a staging directory.
3. Atomically renames the staging directory to the configured `SPECS_DIR`.
4. Builds a new composite router and swaps it in via an atomic pointer.

The swap is lock-free from the request handler's perspective. In-flight requests complete against the old router; new requests immediately use the refreshed specs.

If the download or extraction fails, the existing router stays in place and the error is logged. The next scheduled run will retry.

---

## Startup and Disk Layout

On startup the proxy runs the same download-and-load sequence as the background refresh. The extracted specs land under `SPECS_DIR` in a subdirectory tree that mirrors the `models/` layout inside the ZIP (one directory per API, each containing one or more JSON files).

The `SPECS_DIR` is the only directory the validation subsystem writes to. It is safe to delete its contents while the proxy is stopped; they will be re-downloaded on the next start.
