# AUP Compliance

Smart Proxy implements structural defenses that directly address several clauses of Amazon's **Acceptable Use Policy (AUP)**. This document is the canonical AUP compliance reference for Smart Proxy. It is structured so an operator can hand it to an AUP auditor.

For DPP-specific compliance (encryption at rest, PII retention, logging without PII, audit log retention), see [docs/DPP_COMPLIANCE.md](DPP_COMPLIANCE.md). The two documents are complementary: DPP governs *how* data is protected; AUP governs *what* data may be used for.

## Contents

1. [Overview](#1-overview)
2. [Shared Responsibility Matrix](#2-shared-responsibility-matrix)
3. [What Smart Proxy enforces](#3-what-smart-proxy-enforces)
4. [What the operator must do (Smart-Proxy-specific)](#4-what-the-operator-must-do-smart-proxy-specific)
5. [Audit preparation checklist (Smart-Proxy-specific)](#5-audit-preparation-checklist-smart-proxy-specific)
6. [Verification table](#6-verification-table)

## 1. Overview

Amazon binds every Solution Provider via the AUP in addition to the DPP. The AUP restricts *purpose* (what data may be used for) and *conduct* (how the API may be called), whereas the DPP restricts *handling* (how data must be stored and protected).

Smart Proxy sits on the request path between an SP-API client and Amazon. It is a transparent reverse proxy: it does not interpret the business intent behind a call, and it cannot prevent an operator's application from misusing data after the response is delivered. What it *can* do is:

- Enforce structural constraints that make certain AUP violations technically difficult (e.g. merchant-scoped cache keys prevent cross-merchant data aggregation at the proxy layer)
- Rate-limit per published SP-API throttle quotas, making quota-circumvention via request duplication less effective
- Redact PII from logs and cache so that data retrieved for one permissible purpose cannot be repurposed from a secondary store
- Provide full per-merchant request visibility via the dashboard so the operator can author AUP §4.9 disclosures

The rest of this document maps each AUP clause to the specific Smart Proxy behavior or operator obligation that addresses it.

## 2. Shared Responsibility Matrix

| AUP § | Requirement | Smart Proxy does | Operator must do |
|---|---|---|---|
| §1.1 | Use API only for acceptable activities on behalf of Authorized Users | nothing | Operator's application logic and Terms of Service |
| §1.2 | Do not facilitate violation of Amazon seller agreements | nothing | Operator's application-level controls |
| §1.3 | Report and block Authorized Users violating Amazon agreements | nothing | Operator's abuse-detection + spapi-abuse@amazon.com reporting |
| §1.4 | Comply with API-specific policies | nothing | Operator's review of applicable sub-policies (§5) |
| §2.1 | No false advertising of the Application | nothing | Operator's marketing materials |
| §2.2 | Be clear about what data is accessed and for what purpose | Dashboard provides full per-merchant request visibility | Operator's privacy notice to Authorized Users |
| §2.3 | Do not deceive Authorized Users by modifying Information | Original SP-API response forwarded to client byte-for-byte; PII redaction applies only to internal logs/cache | Operator must not transform responses before passing to Authorized Users |
| §2.4 | Disclose use of AI/ML models and data freshness | nothing | Operator's AI/analytics disclosure |
| §2.5 | Comply with applicable data-privacy laws (GDPR etc.) | PII redacted in logs/cache; DPP-conformant defaults | Operator's legal basis, data-rights flows, DPA agreements |
| §2.6 | Do not infringe intellectual property | nothing | Operator's IP review |
| §2.7 | Provide required availability and performance | nothing | Operator's SLA, uptime monitoring |
| §2.8 | Mitigate negative impact before new features | nothing | Operator's pre-launch testing |
| §2.9 | Respect per-Authorized-User throttling quotas | Per-merchant token-bucket rate limiter mirrors SP-API published limits; `SP_PROXY_RATELIMIT_THROTTLE_FACTOR=0.8` default safety margin | Do not disable rate limiting in production; set `SP_PROXY_STRICT_MERCHANT=true` so anonymous callers cannot dilute a seller's bucket |
| §2.10 | Data integrity checks for analytical processing | nothing | Operator's validation logic for AI/analytics pipelines |
| §3.1 | Never share access keys or passwords | LWA/RDT tokens held in process memory only; never written to disk, logs, or cache | Operator's secret management (rotation, vaults) |
| §3.2 | Never request another party's access keys | nothing | Operator's onboarding flow |
| §3.3 | Do not request Amazon Portal credentials | nothing | Operator's onboarding flow |
| §3.4 | Act only on behalf of Authorized Users who granted permission | nothing | Operator's OAuth / LWA authorization flow |
| §3.5 | Do not apply for unused access keys | nothing | Operator's credential lifecycle management |
| §3.6 | Do not circumvent policies by asking users to share Portal data manually | nothing | Operator's product design |
| §3.7 | Use secondary user permissions for Portal access | nothing | Operator's Portal integration |
| §3.8 | Do not retrieve Information beyond Application's functional need | nothing | Operator's API-call selection and scope |
| §3.9 | Need-to-know access within organization | Dashboard access control (bind to loopback or put behind auth) | Operator's RBAC for dashboard and data |
| §3.10 | Do not circumvent throttling via multiple Solution Provider accounts | Per-merchant rate limiting is merchant-key-stable across token rotations (not per-account) | Do not create additional SP-API developer accounts to pool quota |
| §3.11 | Notify Amazon of organizational changes within 30 days | nothing | Operator's written org-change policy |
| §3.12 | Disclose affiliated entities when requesting additional roles | nothing | Operator's Amazon developer account management |
| §3.13 | Comply with the DPP | See [docs/DPP_COMPLIANCE.md](DPP_COMPLIANCE.md) | See [docs/DPP_COMPLIANCE.md](DPP_COMPLIANCE.md) |
| §4.1 | PII only for fulfillment or legal requirements; document processing with Authorized Users | PII redacted from logs/cache; original response not modified (proxy does not know call purpose) | Operator's documented lawful purpose; Authorized User PII processing agreement |
| §4.2 | No customer targeting for marketing or review manipulation | nothing | Operator's application-layer controls |
| §4.3 | No vending of Amazon data via external data services | nothing | Operator's product and data-resale policy |
| §4.4 | No cross-Authorized-User data aggregation | Merchant-scoped cache keys structurally prevent cross-merchant cache sharing; audit log records merchant key per request | Operator's application must not aggregate SP-API data across seller accounts for resale or competitive analysis |
| §4.5 | No publishing of insights about Amazon's business | nothing | Operator's analytics and publishing policy |
| §4.6 | No disclosure of Information to third parties beyond permissible activities | When `SP_PROXY_BODIES_BACKEND=s3`, bodies land on the S3 provider; this counts as a disclosure to an affiliated entity | Operator's vendor-management process; S3 provider DPA; DPP §2.8 subcontractor review |
| §4.7 | Due diligence on data security of parties data is shared with | nothing (proxy cannot evaluate third-party security) | Operator's vendor due diligence for S3 provider and any analytics/logging sinks |
| §4.8 | Contractual basis for PII transfers | nothing | Operator's DPA / SCCs for cross-border PII transfers |
| §4.9 | Transparency with Authorized Users about data sharing | Dashboard provides per-merchant request log and audit trail as operational basis | Operator's privacy notice: what is stored, where, how long, with whom |
| §5.1–5.2 | Buyer-Seller Messaging compliance | Messaging endpoint registered in PII registry; MessageText/Attachments redacted from logs | Operator's Communication Guidelines compliance; Messaging API template support |
| §5.3 | Merchant Fulfillment API Service Terms | MFN endpoints registered in PII registry; ShipTo/ShipFrom addresses redacted from logs | Operator's Merchant Fulfillment API Service Terms compliance |
| §5.4 | Amazon Freight Services API Terms | nothing | Operator's Amazon Freight Services API Terms compliance |
| §5.5 | Amazon Business API Terms | nothing | Operator's Technology Integration Agreement / Amazon Business Account Terms compliance |
| §5.6–5.7 | Page View / End User Data Report Terms (EU) | nothing | Operator's EU-specific report terms compliance |

## 3. What Smart Proxy enforces

### 3.1 Rate limiting within published SP-API quotas (AUP §2.9, §3.10)

Smart Proxy maintains a **per-endpoint, per-merchant token bucket** that mirrors SP-API's published rate limits. The default throttle factor is `SP_PROXY_RATELIMIT_THROTTLE_FACTOR=0.8`, leaving a 20% safety margin below the published limit to absorb burst variance.

Merchant identity is resolved from the `X-SP-Proxy-Merchant-Id` header (or a SHA-256 hash of the access token as fallback). Because the bucket key is **merchant-stable across token rotations**, the rate limiter tracks the true per-seller call rate even as LWA tokens rotate hourly. This directly serves AUP §2.9 (respect per-Authorized-User throttle quotas): a seller's quota is tracked against the seller, not against a token that changes every hour.

AUP §3.10 prohibits circumventing throttle quotas by creating multiple Solution Provider accounts. Smart Proxy cannot prevent an operator from holding multiple SP-API developer accounts, but the per-merchant rate limiting means that any calls from a given merchant key -- regardless of which SP-API developer account's token is used -- draw from the same bucket. Splitting tokens across accounts while sending all traffic through a single Smart Proxy instance does not multiply quota at the proxy layer.

Three rate-limit modes are available:
- `queue` (default): requests wait for a token to become available
- `reject`: requests exceeding the bucket return HTTP 429 immediately
- `queue-timeout`: requests wait up to `SP_PROXY_RATELIMIT_QUEUE_TIMEOUT` (default 60s), then 429

*Code: bucket construction + token-bucket algorithm in [internal/ratelimit/](../internal/ratelimit/); per-merchant key resolution in [internal/merchant/](../internal/merchant/); throttle factor + mode defaults in [internal/config/config.go](../internal/config/config.go).*

### 3.2 Merchant-scoped cache keys (AUP §4.4)

AUP §4.4 prohibits aggregating data across Authorized Users' businesses. Smart Proxy's cache key always includes the resolved merchant key, so a cache entry written for Seller A can never be served to Seller B. In multi-tenant deployments -- where a single Smart Proxy instance serves multiple seller accounts -- this is a structural guarantee rather than a policy.

The `X-SP-Proxy-Cache: PII_EXCLUDED` response header indicates that a response was withheld from cache because it contained PII. PII responses are never stored, so they cannot leak from one merchant's cache lookup into another merchant's request.

*Code: cache key construction in [internal/cache/keys.go](../internal/cache/keys.go); PII exclusion in [internal/cache/middleware.go](../internal/cache/middleware.go).*

### 3.3 Token security (AUP §3.1)

LWA access tokens and RDTs are held **in process memory only**. They are never written to SQLite, body files, or any other persistent store. Header redaction (see [docs/DPP_COMPLIANCE.md §3.4](DPP_COMPLIANCE.md#34-header-redaction)) ensures that `Authorization`, `x-amz-access-token`, and `x-amz-security-token` are stripped from all persisted log records before they reach disk.

*Code: RDT in-memory store in [internal/rdt/](../internal/rdt/); header redaction in [internal/pii/headers.go](../internal/pii/headers.go).*

### 3.4 Original response forwarded unmodified (AUP §2.3)

AUP §2.3 prohibits deceiving Authorized Users through deliberate modification of Information. Smart Proxy's PII engine operates on a **copy** of the captured response; the original bytes are forwarded to the client unchanged before any redaction runs. This is a structural guarantee: `RedactForLogging` returns a new object and never mutates the input.

*Code: copy-before-redact in [internal/pii/engine.go](../internal/pii/engine.go) (`RedactForLogging`); request flow in [internal/proxy/](../internal/proxy/) writes original response to client before [internal/logging/](../internal/logging/) hands the captured copy to the engine.*

### 3.5 Per-request audit trail (AUP §2.2, §4.9)

Every proxied request is recorded in `request_logs` with merchant key, endpoint, method, status code, cache outcome, and timestamp. The dashboard exposes this log with full filtering so an operator can:

- Verify which data was accessed on behalf of which seller (AUP §2.2 -- be clear about what data is accessed)
- Identify all data flows to include in the §4.9 Authorized User transparency disclosure
- Detect anomalous call patterns (unexpected endpoints, unexpected merchants) for §1.3 abuse monitoring

The audit log separately records system events (boot, config warnings, operator actions) with a retention default of ~13 months (`SP_PROXY_PURGE_AUDIT_RETENTION=9504h`).

*Code: request logging in [internal/logging/](../internal/logging/); audit log in [internal/audit/](../internal/audit/); dashboard in [web/](../web/).*

### 3.6 S3 body storage as a data-disclosure event (AUP §4.6)

When `SP_PROXY_BODIES_BACKEND=s3`, response bodies (PII-redacted before write) are uploaded to an external object store. Under AUP §4.6, this constitutes disclosure of Information to an affiliated entity. Smart Proxy enforces two technical safeguards:

1. **PII is redacted before upload.** Bodies pass through the PII engine before `PutObject`, so raw PII never leaves the proxy host over the S3 connection.
2. **TLS is enforced.** Plain `http://` S3 endpoints are refused in production mode (`SP_PROXY_ENV=production`) to prevent credential and payload exposure in transit.

The operator is responsible for the S3 provider's DPA, vendor due diligence (AUP §4.7), and DPP §2.8 subcontractor review.

*Code: SSE enforcement + plain-http refusal in [internal/config/config.go](../internal/config/config.go) `Validate()`; PII-before-upload path in [internal/bodies/](../internal/bodies/) → [internal/pii/engine.go](../internal/pii/engine.go); S3 client in [internal/blob/s3.go](../internal/blob/s3.go).*

## 4. What the operator must do (Smart-Proxy-specific)

This section covers only the touch points where Smart Proxy choices interact with AUP operator obligations. Generic AUP duties (purpose limitation, no marketing use of PII, no data aggregation for resale, organizational change notification) are operator obligations regardless of Smart Proxy and are out of scope here.

### 4.1 Enable `SP_PROXY_STRICT_MERCHANT` in production (AUP §2.9)

By default, requests that supply neither `X-SP-Proxy-Merchant-Id` nor `X-Amz-Access-Token` are assigned to a shared anonymous bucket. In multi-tenant or multi-seller deployments this means anonymous requests share rate-limit quota with identified sellers, which can cause identified sellers to be throttled by unattributed traffic.

Set `SP_PROXY_STRICT_MERCHANT=true` to reject (HTTP 400) any request that cannot be attributed to a merchant. This ensures every rate-limit bucket maps to a real Authorized User.

```
SP_PROXY_STRICT_MERCHANT=true
```

### 4.2 Do not disable rate limiting (AUP §2.9, §3.10)

`SP_PROXY_RATELIMIT_ENABLED=true` is the default. Setting it to `false` removes the per-merchant throttle enforcement. In production, rate limiting must remain enabled to stay within SP-API's per-Authorized-User quotas (AUP §2.9) and to avoid quota circumvention (AUP §3.10).

### 4.3 Dashboard access control and §4.9 disclosure (AUP §2.2, §4.9)

The dashboard (port 9090) shows request logs including endpoints, merchant keys, query parameters, and response bodies (PII-redacted). This data is what AUP §4.9 requires you to disclose to Authorized Users: what data you access, with whom, and for what purpose.

Two obligations follow:

1. **Protect the dashboard.** It ships without authentication and binds to `127.0.0.1` by default. If it is reachable beyond loopback, place an authenticating reverse proxy (mTLS, OAuth, IP allowlist) in front. An unauthenticated dashboard would expose one Authorized User's request data to anyone with network access -- a direct AUP §4.6 violation.

2. **Author a §4.9 disclosure.** Use the dashboard's request log as the factual basis: which endpoints are called, that bodies are stored in Smart Proxy's body store (and optionally in S3), that data is retained for 30 days by default, and who the S3 provider is (if applicable). This disclosure must reach each Authorized User.

### 4.4 PII purpose limitation and documentation (AUP §4.1)

AUP §4.1 requires that PII be used only for merchant-fulfilled shipping or legal requirements, and that you document with Authorized Users any requirement to process PII. Smart Proxy redacts PII from its internal stores, but it has no way to enforce purpose limitation in your application after the response is delivered.

The operator must:

- Document in the Authorized User agreement the specific PII processing purposes (e.g. "we store your order shipping addresses to generate shipping labels")
- Not pass PII from SP-API responses to analytics, advertising, or other pipelines outside the documented purposes
- If using Auto-RDT (`SP_PROXY_RDT_AUTO_MINT=true`), note that RDT minting is a proxy-side technical mechanism; the operator's application is still bound by AUP §4.1 for any PII in the responses those RDTs unlock

### 4.5 S3 provider vendor management (AUP §4.6, §4.7)

When `SP_PROXY_BODIES_BACKEND=s3`, the S3 provider receives PII-redacted bodies. The operator must:

- Execute a Data Processing Agreement (DPA) with the S3 provider
- Verify that the S3 provider's security standards are at least as strict as your own (AUP §4.7)
- Include the S3 provider in the DPP §2.8 subcontractor review
- Ensure the S3 provider is named in the §4.9 Authorized User disclosure ("your data is stored in [provider]")

Set `SP_PROXY_S3_SSE` to enforce server-side encryption (see [docs/DPP_COMPLIANCE.md §4.3](DPP_COMPLIANCE.md#43-s3-server-side-encryption)) as the minimum technical control required before sharing data with the provider.

### 4.6 API-specific policy compliance (AUP §5)

Smart Proxy registers Buyer-Seller Messaging and MFN Shipment endpoints in its PII registry and redacts their sensitive fields from logs. This covers the *data handling* dimension of §5.1--5.3. The *behavioral* compliance obligations remain with the operator:

| AUP § | Operator action required |
|---|---|
| §5.1–5.2 | Review applicable Amazon Communication Guidelines for each marketplace; integrate with the Messaging and Solicitations API for approved templates |
| §5.3 | Review and comply with Merchant Fulfillment API Service Terms |
| §5.4 | Review and comply with Amazon Freight Services API Terms (US) |
| §5.5 | Execute Technology Integration Agreement or accept Amazon Business Account Terms as applicable |
| §5.6 | Review Page View Report Terms (EU) if using that report |
| §5.7 | Review End User Data Report Terms (EU) if using that report |

## 5. Audit preparation checklist (Smart-Proxy-specific)

Generic AUP duties (purpose limitation, no marketing use, org-change notification, vendor reviews) are universal obligations; this checklist covers only what the operator must verify **about Smart Proxy** before an AUP audit.

- [ ] `SP_PROXY_STRICT_MERCHANT=true` is set in production
- [ ] `SP_PROXY_RATELIMIT_ENABLED=true` is set (default; verify not overridden)
- [ ] `SP_PROXY_RATELIMIT_THROTTLE_FACTOR` is ≤ 1.0 and not increased beyond 1.0 (would exceed published SP-API limits)
- [ ] Dashboard not publicly reachable, or authenticating reverse proxy confirmed in front
- [ ] If `SP_PROXY_BODIES_BACKEND=s3`: DPA executed with S3 provider; provider named in §4.9 disclosure
- [ ] If `SP_PROXY_BODIES_BACKEND=s3`: `SP_PROXY_S3_SSE` is set; `SP_PROXY_S3_ENDPOINT` uses `https://`
- [ ] §4.9 disclosure authored and delivered to all Authorized Users (storage location, retention period, S3 provider if applicable)
- [ ] AUP §4.1 purpose-limitation documented with Authorized Users for any PII processing
- [ ] If Auto-RDT enabled (`SP_PROXY_RDT_AUTO_MINT=true`): PII returned via RDT-gated endpoints is covered in §4.1 documentation
- [ ] Applicable §5 sub-policies reviewed and compliance confirmed for each API used

<details>
<summary>Verification commands</summary>

```bash
# Confirm strict-merchant is active (startup config dump).
docker logs smart-proxy 2>&1 | grep -i strict_merchant

# Confirm rate limiting is active and throttle factor is within bounds.
docker logs smart-proxy 2>&1 | grep -i ratelimit

# Confirm no rate-limit bucket is shared across merchant keys
# (each seller should have its own row in the bucket TTL map -- not directly
# observable via CLI, but the dashboard metrics endpoint exposes per-merchant
# request counts that can be compared against SP-API quota consumption).
curl -s http://localhost:9090/metrics | grep sp_proxy_requests_total

# Confirm dashboard is not reachable from outside the host.
curl --max-time 3 http://<external-ip>:9090/
# Expected: Connection refused or firewall timeout

# Audit log: check for any dpp_compliance_warning that is also AUP-relevant
# (non-loopback dashboard, disabled fail-closed, disabled PII cache exclusion).
sqlite3 /data/sp-proxy.db \
  "SELECT timestamp, event_type, message FROM audit_log \
   WHERE event_type='dpp_compliance_warning' \
   ORDER BY timestamp DESC LIMIT 20;"

# Confirm S3 endpoint uses HTTPS (if S3 backend is in use).
docker inspect smart-proxy \
  --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep SP_PROXY_S3_ENDPOINT
# Expected value must start with https://

# Confirm S3 SSE is set (if S3 backend is in use).
docker inspect smart-proxy \
  --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep SP_PROXY_S3_SSE
# Expected: AES256, aws:kms, or aws:kms:dsse
```

</details>

## 6. Verification table

Each Smart-Proxy AUP enforcement claim above is backed by code and (in almost all cases) a test. Reviewers can run these tests to verify the claim independently.

| Smart Proxy enforcement | AUP § | Implementation | Verified by |
|---|---|---|---|
| Per-merchant token-bucket rate limiting | §2.9, §3.10 | [internal/ratelimit/](../internal/ratelimit/); merchant key in [internal/merchant/](../internal/merchant/) | Unit tests in [internal/ratelimit/](../internal/ratelimit/); end-to-end in [test/e2e/ratelimit_test.go](../test/e2e/ratelimit_test.go) |
| Throttle factor default 0.8 | §2.9 | [internal/config/config.go](../internal/config/config.go) | `TestDefaults_ThrottleFactor` in [internal/config/config_test.go](../internal/config/config_test.go) |
| Merchant-scoped cache keys | §4.4 | [internal/cache/keys.go](../internal/cache/keys.go), [internal/cache/middleware.go](../internal/cache/middleware.go) | [internal/cache/keys_test.go](../internal/cache/keys_test.go); end-to-end in [test/e2e/merchant_test.go](../test/e2e/merchant_test.go) |
| PII excluded from cache (`PII_EXCLUDED` header) | §4.4 | [internal/cache/middleware.go](../internal/cache/middleware.go) | [internal/cache/middleware_test.go](../internal/cache/middleware_test.go), [test/e2e/cache_test.go](../test/e2e/cache_test.go) |
| Tokens in process memory only (never persisted) | §3.1 | [internal/rdt/](../internal/rdt/) (in-memory map, no persistence); no token writers in [internal/storage/](../internal/storage/) or [internal/bodies/](../internal/bodies/) | Structural: verified by absence of token persistence; header redaction covered by [internal/pii/headers_test.go](../internal/pii/headers_test.go) |
| Header redaction (Authorization, x-amz-access-token, x-amz-security-token) | §3.1 | [internal/pii/headers.go](../internal/pii/headers.go) | [internal/pii/headers_test.go](../internal/pii/headers_test.go); `TestE2E_PII_AuthHeaderRedacted` in [test/e2e/pii_test.go](../test/e2e/pii_test.go) |
| Original response forwarded unmodified | §2.3 | [internal/pii/engine.go](../internal/pii/engine.go) (`RedactForLogging` returns a copy); [internal/proxy/](../internal/proxy/) | `TestRedactForLogging_OriginalUnmodified` in [internal/pii/engine_test.go](../internal/pii/engine_test.go); `TestE2E_DPP_NoLeakOfBuyerEmail` in [test/e2e/dpp_test.go](../test/e2e/dpp_test.go) |
| Messaging endpoint PII redaction (MessageText, Attachments) | §5.1–5.2 | [internal/pii/registry.go](../internal/pii/registry.go) (messaging rules) | [internal/pii/registry_test.go](../internal/pii/registry_test.go) |
| MFN shipment address redaction (ShipTo/ShipFrom) | §5.3 | [internal/pii/registry.go](../internal/pii/registry.go) (mfn rules) | [internal/pii/registry_test.go](../internal/pii/registry_test.go) |
| PII redacted before S3 upload | §4.6 | [internal/bodies/](../internal/bodies/) → [internal/pii/engine.go](../internal/pii/engine.go); [internal/blob/s3.go](../internal/blob/s3.go) | [internal/blob/s3_test.go](../internal/blob/s3_test.go) |
| Plain-`http://` S3 endpoint refused in production | §4.6 | [internal/config/config.go](../internal/config/config.go) `Validate()` | `TestValidate_S3InsecureEndpointBlockedInProd` in [internal/config/config_test.go](../internal/config/config_test.go) |
| Per-request audit trail (merchant key, endpoint, outcome) | §2.2, §4.9 | [internal/logging/](../internal/logging/); [internal/audit/](../internal/audit/) | Integration tests in [test/e2e/](../test/e2e/) |
