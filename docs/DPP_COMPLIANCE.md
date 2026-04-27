# DPP & AUP Compliance

Smart Proxy implements built-in support for Amazon's **Data Protection Policy (DPP)** and **Acceptable Use Policy (AUP)** by detecting and redacting Personally Identifiable Information (PII) before it reaches logs, caches, or persistent storage, and by shipping with DPP-conformant defaults.

This document is the canonical compliance reference for Smart Proxy. It is structured so an operator can hand it to a DPP/AUP auditor.

## Contents

1. [Overview](#1-overview)
2. [Shared Responsibility Matrix](#2-shared-responsibility-matrix)
3. [What Smart Proxy enforces](#3-what-smart-proxy-enforces)
4. [What the operator must do (Smart-Proxy-specific)](#4-what-the-operator-must-do-smart-proxy-specific)
5. [Configuration reference](#5-configuration-reference)
6. [Audit preparation checklist (Smart-Proxy-specific)](#6-audit-preparation-checklist-smart-proxy-specific)
7. [AUP compliance notes](#7-aup-compliance-notes)
8. [Verification table](#8-verification-table)

## 1. Overview

Amazon binds every Solution Provider via the DPP (substantive security: encryption, retention, logging, access control) and AUP (data-use restrictions). Smart Proxy runs on the request path between an SP-API client and Amazon. It cannot enforce operator-environment requirements (encrypted volumes, MFA, KMS, pen-tests, IR plans), but it **can** ensure that:

- PII is redacted before it lands on disk in logs, metadata, body files, or cache
- Default configuration values are DPP-conformant out of the box
- Operator misconfigurations that weaken DPP posture surface as startup warnings AND as `dpp_compliance_warning` audit events

The rest of this document maps each commitment to the specific code path or configuration that delivers it.

## 2. Shared Responsibility Matrix

| DPP § | Requirement | Smart Proxy does | Operator must do |
|---|---|---|---|
| §1.1 | Network protection (firewalls, NACLs) | nothing | Operator's environment (firewalls, security groups) |
| §1.2 | Access management, MFA, account lockout | nothing | Operator's IdP / OS |
| §1.3 | Least privilege | Merchant-scoped cache keys prevent cross-merchant data leaks | Operator's RBAC for the dashboard |
| §1.4 | Credentials not exposed | Authorization, x-amz-access-token, x-amz-security-token redacted in logs; document URLs (presigned credential-equivalent) redacted | Rotate API keys; do not commit secrets |
| §1.5 | TLS 1.2+ in transit | Go stdlib HTTPS to Amazon | Client to Proxy and Proxy to S3 must be HTTPS |
| §1.6 | Risk management & IR plan | nothing | Operator's IR plan, 24h Amazon notification |
| §1.7 | 30d deletion on Amazon notice (PII), 18mo for non-PII | Configurable purge job; default 30d for bodies, 30d for metadata | Run purge with the merchant filter on receiving an Amazon §1.7 notice (see §4.4) |
| §1.8 | Data attribution | Merchant key labeled on every request log | Operator's data-tagging policy |
| §2.1 | PII <= 30d | `BODIES_ARCHIVE_MAX_AGE=720h` default; cache excludes PII responses | Do not externally back up the bodies path; respect retention |
| §2.2 | Data governance | Dashboard provides visibility for data-processing inventory | Privacy policy, customer consent, data-rights flows |
| §2.3 | Asset management | nothing | Operator's asset inventory, change management |
| §2.4 | Encryption at rest (AES-128+/RSA-2048+) | nothing | Encrypted volume + S3 SSE (see §4) |
| §2.5 | Secure coding | No hardcoded secrets in this codebase; secrets via env vars | Operator's code-review and secret-scanning |
| §2.6 | Logging without PII; audit logs >=12 months | Header, body, query-string redaction; FAIL_CLOSED=true default; audit retention default 9504h (~13mo) | Backup of audit log; do not point an external log sink at this DB without redaction |
| §2.7 | Vulnerability management (30d scans, annual pentest) | nothing | Operator's CI scanner / pentest contract |
| §2.8 | Subcontractor reviews | nothing | Operator's vendor-management process (S3 provider counts) |

## 3. What Smart Proxy enforces

### 3.1 PII detection and redaction

Smart Proxy maintains a **registry of PII-containing endpoints** at [internal/pii/registry.go](../internal/pii/registry.go). Detection happens at three levels:

#### Full-body PII endpoints

| Endpoint | Content |
|---|---|
| `/orders/v0/orders/{orderId}/buyerInfo` | Buyer email, name |
| `/orders/v0/orders/{orderId}/address` | Shipping address |
| `/orders/v0/orders/{orderId}/orderItems/buyerInfo` | Item-level buyer info |
| `/messaging/v1/orders/{orderId}/messages/{messageId}` | Buyer/seller messages |

#### Partial PII fields

| API | Redacted Fields |
|---|---|
| Orders v0 (list, single, items) | BuyerEmail, BuyerName, BuyerTaxInfo, ShippingAddress, BuyerCustomizedInfo, GiftMessageText |
| Orders v2026 (list, single) | buyer.* and recipient.* (when `?includedData=BUYER` or `RECIPIENT`) |
| Order Regulated Info | RegulatedInformation.Fields, BuyerInfo, ShippingAddress |
| Shipping v2 (`/shipping/v2/shipments`) | ShipTo (name, address, phone, email) |
| MFN Shipments (`/mfn/v0/shipments`) | ShipToAddress, ShipFromAddress |
| FBA Outbound | DestinationAddress |
| Messaging | MessageText, Attachments |
| Finances | OrderId references |
| Easy Ship | OrderId, PickupSlot |

#### Three redaction modes

- `REDACT` (default): replace value with `[REDACTED]`
- `HASH`: replace with `sha256:<hex>` (deterministic, supports correlation)
- `OMIT`: remove the field entirely

The original (unredacted) response is always forwarded to your application unchanged. Redaction applies only to logs and cache.

### 3.2 Cache exclusion

When `SP_PROXY_CACHE_EXCLUDE_PII=true` (default), responses containing PII are not stored in the cache. The response header `X-SP-Proxy-Cache: PII_EXCLUDED` indicates the exclusion.

### 3.3 Retention enforcement

| Tier | Default | DPP § |
|---|---|---|
| Body files (PII) | 30 days (`BODIES_ARCHIVE_MAX_AGE=720h`) | §2.1 |
| Metadata (request logs, no PII) | 30 days (`PURGE_METADATA_RETENTION=720h`) | §1.7 (limit 18 months) |
| Audit log | ~13 months (`PURGE_AUDIT_RETENTION=9504h`) | §2.6 (minimum 12 months) |

### 3.4 Header redaction

These headers are always redacted in logs:

| Header | Reason |
|---|---|
| `Authorization` | Contains LWA access token |
| `x-amz-access-token` | SP-API access token |
| `x-amz-security-token` | AWS STS session token |

### 3.5 Document-URL redaction

Pre-signed S3 URLs returned by `/reports/2021-06-30/documents/{documentId}`, `/feeds/2021-06-30/documents/{feedDocumentId}`, and `/datakiosk/2023-11-15/documents/{documentId}` are credential-equivalent: anyone with the URL within its ~5-minute validity window can fetch the underlying file (which often contains PII). Smart Proxy redacts the `url` field (and `encryptionDetails.key` where present) in logs.

The original URL is forwarded to the client; only the persisted log copy is redacted. Operators who need to re-download a document after-the-fact re-issue the original API call to mint a fresh URL.

### 3.6 Query-string redaction

Query parameters whose values are PII are redacted in `request_logs.query_params` before SQLite storage. The default list is `buyerEmail` and `buyerName` (case-insensitive). Operators can extend the list via `SP_PROXY_PII_QUERY_PARAMS=foo,bar`.

The original query string is forwarded to Amazon unchanged.

### 3.7 Fail-closed mode (default)

`SP_PROXY_PII_FAIL_CLOSED=true` (default) makes Smart Proxy treat any path that does not match a registered SP-API endpoint as full-body PII. This guarantees that a new SP-API endpoint added by Amazon cannot silently bypass redaction until the registry is updated.

Trade-off: dashboard log-detail views show `{"redacted": true, ...}` for any endpoint not yet in the registry. Update [internal/pii/registry.go](../internal/pii/registry.go) when Amazon adds endpoints you actually use.

## 4. What the operator must do (Smart-Proxy-specific)

This section covers only the touch points where Smart-Proxy choices interact with operator infrastructure. Generic DPP duties (MFA, password policy, vuln scans, pen-tests, vendor reviews, IR plan) are operator obligations regardless of Smart Proxy and are out of scope here.

### 4.1 Encrypted volumes

Smart Proxy persists two artifacts to disk:
- `SP_PROXY_SQLITE_PATH` (default `/data/sp-proxy.db`) -- request metadata, audit log
- `SP_PROXY_BODIES_PATH` (default `/data/bodies`) -- redacted bodies in JSONL files

Bodies are PII-redacted before write, but request metadata (paths, query strings after redaction, status codes, latencies) is in the clear. DPP §2.4 requires encryption at rest. Mount the data volume on encrypted block storage:

- AWS: EBS encryption (default for new volumes since 2023) or encrypted EFS
- GCP: Persistent Disks (encrypted by default)
- Azure: Managed Disks with SSE
- Self-hosted: LUKS, dm-crypt, ZFS native encryption
- Kubernetes: StorageClass backed by an encrypted CSI driver

Tokens (LWA + RDT) live in process memory only.

### 4.2 Dashboard reverse proxy

The dashboard (port 9090) ships without authentication and binds to `127.0.0.1` by default (`SP_PROXY_DASHBOARD_BIND_ADDR=127.0.0.1`). Two deployment shapes:

- **Same-host sidecar:** dashboard reachable only via loopback. No further action needed.
- **Networked dashboard:** set `SP_PROXY_DASHBOARD_BIND_ADDR=0.0.0.0` and place an authenticating reverse proxy (mTLS, OAuth, IP allowlist) in front. The proxy will emit a `dpp_compliance_warning` audit event at startup; this is the audit signal that the operator has acknowledged the requirement.

Container deployments use both: the container binds to `0.0.0.0` internally (so Docker port-forward works), and the host-side mapping `127.0.0.1:9090:9090` enforces the loopback restriction. The startup warning still fires inside the container; treat that as the audit acknowledgment.

### 4.3 S3 server-side encryption

When `SP_PROXY_BODIES_BACKEND=s3`, set `SP_PROXY_S3_SSE` to enforce SSE on every PutObject:

```
SP_PROXY_S3_SSE=AES256          # SSE-S3
SP_PROXY_S3_SSE=aws:kms         # SSE-KMS (set SP_PROXY_S3_SSE_KMS_KEY)
SP_PROXY_S3_SSE=aws:kms:dsse    # dual-layer SSE-KMS
```

Pair with a bucket policy that denies unencrypted uploads:

```json
{
  "Effect": "Deny",
  "Principal": "*",
  "Action": "s3:PutObject",
  "Resource": "arn:aws:s3:::your-bucket/*",
  "Condition": {"StringNotEquals": {"s3:x-amz-server-side-encryption": "AES256"}}
}
```

Smart Proxy refuses to start in production mode if `SP_PROXY_S3_ENDPOINT` is plain `http://`.

### 4.4 §1.7 deletion procedure

When Amazon issues a §1.7 deletion notice for a specific merchant, the operator must purge that merchant's data from Smart Proxy storage. Concrete commands:

```bash
# 1. Stop accepting new requests for the merchant (firewall, app-side switch).

# 2. Purge SQLite request_logs and audit_log for the merchant.
sqlite3 /data/sp-proxy.db <<EOF
DELETE FROM request_logs WHERE merchant_key = 'TARGET_MERCHANT_KEY';
DELETE FROM audit_log WHERE source = 'merchant' AND message LIKE '%TARGET_MERCHANT_KEY%';
VACUUM;
EOF

# 3. Purge JSONL bodies. Bodies are stored by hour, not by merchant; the
#    merchant key is inside each line. You must rewrite the affected files:
for f in /data/bodies/current/*.jsonl /data/bodies/recent/*.jsonl; do
    grep -v '"merchant_key":"TARGET_MERCHANT_KEY"' "$f" > "$f.tmp" && mv "$f.tmp" "$f"
done
# Archive tier is compressed; decompress, filter, recompress as needed.

# 4. Restart the proxy to drop in-memory caches.
docker restart smart-proxy

# 5. Document the deletion (timestamp, merchant key, files touched) for the
#    Amazon §1.7 certification response.
```

Test this procedure on a non-production data set before you need it under time pressure.

## 5. Configuration reference

| Variable | Default | DPP § | Description |
|---|---|---|---|
| `SP_PROXY_ENV` | `development` | n/a | Set to `production` to enable strict validations and DPP warnings |
| `SP_PROXY_PII_FAIL_CLOSED` | `true` | §2.6 | Treat unknown SP-API endpoints as PII |
| `SP_PROXY_PII_QUERY_PARAMS` | (empty) | §2.6 | Extra query-param names treated as PII (case-insensitive, comma-separated) |
| `SP_PROXY_CACHE_EXCLUDE_PII` | `true` | §2.1 | Do not cache PII responses |
| `SP_PROXY_BODIES_ARCHIVE_MAX_AGE` | `720h` | §2.1 | Maximum PII body retention (30d) |
| `SP_PROXY_PURGE_METADATA_RETENTION` | `720h` | §1.7 | Maximum non-PII metadata retention |
| `SP_PROXY_PURGE_AUDIT_RETENTION` | `9504h` | §2.6 | Minimum audit log retention (~13mo) |
| `SP_PROXY_DASHBOARD_BIND_ADDR` | `127.0.0.1` | §1.2 | Dashboard listener bind address |
| `SP_PROXY_S3_SSE` | (empty) | §2.4 | Force S3 server-side encryption (AES256 / aws:kms / aws:kms:dsse) |
| `SP_PROXY_S3_SSE_KMS_KEY` | (empty) | §2.4 | KMS key ARN/alias for SSE=aws:kms |

## 6. Audit preparation checklist (Smart-Proxy-specific)

Generic DPP duties (vuln scans, pen-tests, access review, vendor reviews, IR plan) are universal obligations; this checklist covers only what the operator must verify **about Smart Proxy** before an audit.

- [ ] `SP_PROXY_ENV=production` is set
- [ ] Boot logs show no `dpp_compliance_warning` audit events
- [ ] Dashboard port not publicly reachable (curl from external IP returns Connection Refused)
- [ ] Reverse proxy with auth in front of dashboard (if dashboard exposed beyond loopback)
- [ ] `SP_PROXY_S3_SSE=AES256` (or `aws:kms`) is set, if S3 backend is in use
- [ ] S3 bucket policy "Deny-Unencrypted-Put" active, if S3 backend is in use
- [ ] Volume encryption active for `/data` (verify via cloud console or `lsblk -o NAME,FSTYPE`)
- [ ] Cleartext-PII grep on SQLite DB returns no hits (see commands below)
- [ ] Smart Proxy on a current release version
- [ ] §1.7 deletion procedure (see §4.4) tested on a non-production dataset

<details>
<summary>Verification commands</summary>

```bash
# Cleartext-PII canary in SQLite query_params (should return 0 rows after redaction lands).
sqlite3 /data/sp-proxy.db "SELECT path, query_params FROM request_logs WHERE query_params LIKE '%@%' LIMIT 5;"

# Cleartext-PII canary in JSONL bodies (should return 0 hits).
zgrep -E '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' /data/bodies/archive/*.jsonl.zst 2>/dev/null
grep  -E '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' /data/bodies/recent/*.jsonl   2>/dev/null
grep  -E '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' /data/bodies/current/*.jsonl  2>/dev/null

# Audit-event tail (the boot warnings are at the top of each session).
sqlite3 /data/sp-proxy.db "SELECT timestamp, event_type, message FROM audit_log WHERE event_type='dpp_compliance_warning' ORDER BY timestamp DESC LIMIT 20;"
```

False positives are possible (e.g. seller emails in support-message bodies that pre-date redaction). Investigate any hits.

</details>

## 7. AUP compliance notes

Smart Proxy interacts with AUP at these specific points:

- **§4.1 (PII only for fulfillment).** Not enforced by Smart Proxy. The proxy does not know *why* the operator's code is calling an endpoint. Operators must ensure that PII obtained via Smart Proxy is used only for fulfillment, tax, legal, or order communication purposes.
- **§4.4 (no cross-merchant aggregation).** Smart Proxy's merchant-scoped cache keys (see [docs/CACHING.md](CACHING.md)) prevent unintentional data leakage between merchant accounts in multi-tenant deployments. This is a structural defense, not an enforcement mechanism for the operator's own application logic.
- **§4.6 (no third-party disclosure).** When using `SP_PROXY_BODIES_BACKEND=s3`, the S3 provider becomes a subcontractor with respect to PII. Treat them accordingly in your vendor-management process.
- **§3.10 (no multi-account throttle circumvention).** Smart Proxy's merchant-key-stable rate limiting (see [docs/RATE_LIMITING.md](RATE_LIMITING.md)) is a per-merchant efficiency optimization across token rotations within one merchant. It is not multi-account aggregation. Auditors who read "stable buckets across token rotations" should understand this is *within* a merchant, not across merchants.
- **§2.x (transparency).** The dashboard provides operators with visibility into every API call: which merchant, which endpoint, when, with what cache outcome. This visibility is what operators need to author the AUP §4.9 disclosure to their own users.

## 8. Verification table

Each Smart-Proxy enforcement claim above is backed by a test. Reviewers can run these tests to verify the claim independently.

| Smart Proxy enforcement | Verified by |
|---|---|
| PII header redaction (§3.4) | [internal/pii/headers_test.go](../internal/pii/headers_test.go) |
| Field-rule redaction (§3.1) | [internal/pii/registry_test.go](../internal/pii/registry_test.go) |
| Document-URL redaction (§3.5) | `TestDocumentURL_RedactionRoundTrip` |
| Query-string redaction (§3.6) | [internal/pii/query_test.go](../internal/pii/query_test.go) |
| Cache exclusion on PII (§3.2) | [internal/cache/](../internal/cache/) |
| FAIL_CLOSED default (§3.7) | `TestDefaults_FailClosedTrue` |
| 30-day body retention default (§3.3) | existing config tests |
| 13-month audit retention default (§3.3) | `TestDefaults_AuditRetention13Months` |
| Production-mode warnings emit | `TestProductionWarnings_*` |
| Dashboard loopback default | `TestDefaults_DashboardBindAddrLoopback`, `TestDashboardBindAddr_DefaultsToLoopback` |
| End-to-end PII no-leak | [test/e2e/dpp_test.go](../test/e2e/dpp_test.go) |
