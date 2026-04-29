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

*Code: rules + full-body endpoint list in [internal/pii/registry.go](../internal/pii/registry.go); modes + non-mutating `RedactForLogging` in [internal/pii/engine.go](../internal/pii/engine.go); JSONPath-style field selection in [internal/pii/jsonpath.go](../internal/pii/jsonpath.go).*

### 3.2 Cache exclusion

When `SP_PROXY_CACHE_EXCLUDE_PII=true` (default), responses containing PII are not stored in the cache. The response header `X-SP-Proxy-Cache: PII_EXCLUDED` indicates the exclusion.

*Code: [internal/cache/middleware.go](../internal/cache/middleware.go), default in [internal/config/config.go](../internal/config/config.go) (`SP_PROXY_CACHE_EXCLUDE_PII`).*

### 3.3 Retention enforcement

| Tier | Default | DPP § |
|---|---|---|
| Body files (PII) | 30 days (`BODIES_ARCHIVE_MAX_AGE=720h`) | §2.1 |
| Metadata (request logs, no PII) | 30 days (`PURGE_METADATA_RETENTION=720h`) | §1.7 (limit 18 months) |
| Audit log | ~13 months (`PURGE_AUDIT_RETENTION=9504h`) | §2.6 (minimum 12 months) |

*Code: defaults in [internal/config/config.go](../internal/config/config.go); body rotation in [internal/bodies/rotator.go](../internal/bodies/rotator.go); metadata + audit purge in [internal/purge/purge.go](../internal/purge/purge.go).*

### 3.4 Header redaction

These headers are always redacted in logs:

| Header | Reason |
|---|---|
| `Authorization` | Contains LWA access token |
| `x-amz-access-token` | SP-API access token |
| `x-amz-security-token` | AWS STS session token |

*Code: [internal/pii/headers.go](../internal/pii/headers.go), invoked from [internal/logging/](../internal/logging/) before metadata is persisted.*

### 3.5 Document-URL redaction

Pre-signed S3 URLs returned by `/reports/2021-06-30/documents/{documentId}`, `/feeds/2021-06-30/documents/{feedDocumentId}`, and `/datakiosk/2023-11-15/documents/{documentId}` are credential-equivalent: anyone with the URL within its ~5-minute validity window can fetch the underlying file (which often contains PII). Smart Proxy redacts the `url` field (and `encryptionDetails.key` where present) in logs.

The original URL is forwarded to the client; only the persisted log copy is redacted. Operators who need to re-download a document after-the-fact re-issue the original API call to mint a fresh URL.

*Code: rules in [internal/pii/registry.go](../internal/pii/registry.go) (`reports`, `feeds`, `datakiosk` document entries); applied via [internal/pii/engine.go](../internal/pii/engine.go) `RedactForLogging` (returns a redacted copy, never mutates the original bytes); request flow in [internal/proxy/](../internal/proxy/) writes the original response to the client before [internal/logging/](../internal/logging/) hands the captured copy to the engine.*

### 3.6 Query-string redaction

Query parameters whose values are PII are redacted in `request_logs.query_params` before SQLite storage. The default list is `buyerEmail` and `buyerName` (case-insensitive). Operators can extend the list via `SP_PROXY_PII_QUERY_PARAMS=foo,bar`.

The original query string is forwarded to Amazon unchanged.

*Code: [internal/pii/query.go](../internal/pii/query.go); operator extras wired through [internal/pii/registry.go](../internal/pii/registry.go); applied in [internal/logging/](../internal/logging/) on the persisted copy only.*

### 3.7 Fail-closed mode (default)

`SP_PROXY_PII_FAIL_CLOSED=true` (default) makes Smart Proxy treat any path that does not match a registered SP-API endpoint as full-body PII. This guarantees that a new SP-API endpoint added by Amazon cannot silently bypass redaction until the registry is updated.

Trade-off: dashboard log-detail views show `{"redacted": true, ...}` for any endpoint not yet in the registry. Update [internal/pii/registry.go](../internal/pii/registry.go) when Amazon adds endpoints you actually use.

*Code: default + flag wiring in [internal/config/config.go](../internal/config/config.go); fail-closed lookup logic in [internal/pii/registry.go](../internal/pii/registry.go).*

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

#### Production-mode warnings (`dpp_compliance_warning`)

The non-loopback dashboard warning is one entry in a broader set. Smart Proxy evaluates the full configuration on startup and emits a `dpp_compliance_warning` audit event for each finding. In `SP_PROXY_ENV=production` the following warnings fire:

| Trigger | DPP § |
|---|---|
| `SP_PROXY_PII_FAIL_CLOSED=false` | §2.6 (logging without PII) |
| `SP_PROXY_CACHE_EXCLUDE_PII=false` | §2.1 (PII retention) |
| `SP_PROXY_BODIES_ARCHIVE_MAX_AGE > 30d` | §2.1 |
| `SP_PROXY_PURGE_METADATA_RETENTION > 18 months` | §1.7 |
| `SP_PROXY_PURGE_AUDIT_RETENTION < 12 months` | §2.6 |
| `SP_PROXY_DASHBOARD_BIND_ADDR` is non-loopback | §1.2 (access management) |

Two further warnings fire in **all** environments (not gated on production) when the S3 backend is enabled:

| Trigger | DPP § |
|---|---|
| `SP_PROXY_S3_ENDPOINT` uses plain `http://` | §1.5 (TLS in transit) |
| `SP_PROXY_S3_SSE` is empty (relying on bucket default) | §2.4 (encryption at rest) |

A clean production boot prints zero `dpp_compliance_warning` entries. Auditors should grep for the event type as part of pre-audit verification (see §6).

*Code: default bind address + warning logic in [internal/config/config.go](../internal/config/config.go) `Warnings()`; listener construction in [internal/server/server.go](../internal/server/server.go); audit-event emission in [cmd/smart-proxy/main.go](../cmd/smart-proxy/main.go) using `audit.EventDPPComplianceWarning` from [internal/audit/events.go](../internal/audit/events.go); test coverage in `TestProductionWarnings_*` family in [internal/config/config_test.go](../internal/config/config_test.go).*

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

*Code: SSE wiring + `PutObject` enforcement in [internal/blob/s3.go](../internal/blob/s3.go); SSE value validation and plain-`http://` production refusal in [internal/config/config.go](../internal/config/config.go) `Validate()`.*

### 4.4 §1.7 deletion procedure

When Amazon issues a §1.7 deletion notice for a specific merchant, the operator must purge that merchant's data from Smart Proxy storage. The procedure below is correct against the storage layout described in [docs/STORAGE.md](STORAGE.md): SQLite holds `request_logs` rows that point at JSONL body files via `body_file`, `body_offset`, `body_length`. The merchant key is stored only in SQLite; JSONL body entries do not carry it.

**Important constraints to read before running the procedure:**

- JSONL files are append-only and pack multiple merchants' bodies into the same hourly file. There is no in-place "delete only this merchant's bytes" tool today; the operator chooses between (a) deleting the affected files entirely (coarse: also drops other merchants' bodies in the same hour) or (b) rewriting the file by filtering on the entry IDs that belong to the merchant.
- The audit log is a separate concern. DPP §1.7 requires deletion of *PII*, not deletion of the audit trail that proves PII was processed. Smart Proxy audit-log messages are operator events (boot warnings, config changes, purge-job runs); they do not contain request payloads. Preserve audit_log entries as compliance evidence; do not blanket-delete them.

**Concrete commands:**

```bash
# 1. Stop accepting new requests for the merchant (firewall or app-side switch).

# 2. Identify the JSONL files and entry IDs that hold the merchant's bodies.
sqlite3 /data/sp-proxy.db <<EOF
.headers on
.mode csv
SELECT id, body_file, body_offset, body_length
FROM request_logs
WHERE merchant_key = 'TARGET_MERCHANT_KEY'
  AND body_file != '';
EOF
# Save the output. Group rows by body_file: each file may need rewriting
# OR (if no other merchants used the proxy in that hour) outright deletion.

# 3. Rewrite each affected JSONL file, dropping lines whose "id" matches a
#    merchant request id from step 2. Example using jq for one file:
#
#    ids=$(sqlite3 /data/sp-proxy.db "SELECT id FROM request_logs \
#       WHERE merchant_key='TARGET_MERCHANT_KEY' \
#       AND body_file='2026-04-27-14.jsonl';" | paste -sd, -)
#    jq -c --arg ids "$ids" 'select(($ids|split(","))|index(.id)|not)' \
#       /data/bodies/current/2026-04-27-14.jsonl > /tmp/clean.jsonl
#    mv /tmp/clean.jsonl /data/bodies/current/2026-04-27-14.jsonl
#
# Repeat for every file in step 2's output. The recent/ tier holds plain
# JSONL; the archive/ tier is zstd-compressed and must be decompressed,
# filtered, recompressed (zstd -d FILE.zst | jq ... | zstd -o FILE.zst).

# 4. Drop the SQLite metadata rows AND clear any remaining body pointers
#    that point at files we just rewrote. The audit_log is preserved.
sqlite3 /data/sp-proxy.db <<EOF
DELETE FROM request_logs WHERE merchant_key = 'TARGET_MERCHANT_KEY';
VACUUM;
EOF

# 5. Restart the proxy to drop in-memory caches.
docker restart smart-proxy

# 6. Append a §1.7 compliance event to the audit log (NOT to request_logs)
#    so the audit trail records the deletion. This is the evidence Amazon
#    will ask for in the §1.7 certification response.
sqlite3 /data/sp-proxy.db <<EOF
INSERT INTO audit_log (id, timestamp, source, event_type, message, metadata)
VALUES (
  lower(hex(randomblob(16))),
  datetime('now'),
  'operator',
  'dpp_section_1_7_deletion',
  'Deletion completed for merchant TARGET_MERCHANT_KEY',
  json_object('merchant_key', 'TARGET_MERCHANT_KEY')
);
EOF
```

Verify the deletion was complete:

```bash
# 7. Verify: SQLite has no request_logs rows for the merchant.
sqlite3 /data/sp-proxy.db \
  "SELECT count(*) FROM request_logs WHERE merchant_key = 'TARGET_MERCHANT_KEY';"
# Expected: 0

# 8. Verify: no JSONL line carries an id that previously belonged to the
#    merchant. (This is a probabilistic check; rely on step 2's id list.)
```

**Until an automated `smart-proxy purge --merchant=...` CLI lands, test this procedure end-to-end on a non-production data set every quarter.** A deletion notice that is partially honored is worse than one that is not honored at all: it creates a paper trail of compliance that does not match disk state.

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

Each Smart-Proxy enforcement claim above is backed by code and (in almost all cases) a test. Reviewers can run these tests to verify the claim independently.

| Smart Proxy enforcement | Implementation | Verified by |
|---|---|---|
| PII header redaction (§3.4) | [internal/pii/headers.go](../internal/pii/headers.go) | [internal/pii/headers_test.go](../internal/pii/headers_test.go); end-to-end `TestE2E_PII_AuthHeaderRedacted` in [test/e2e/pii_test.go](../test/e2e/pii_test.go) |
| Field-rule redaction (§3.1) | [internal/pii/registry.go](../internal/pii/registry.go), [internal/pii/engine.go](../internal/pii/engine.go), [internal/pii/jsonpath.go](../internal/pii/jsonpath.go) | [internal/pii/registry_test.go](../internal/pii/registry_test.go), [internal/pii/engine_test.go](../internal/pii/engine_test.go) |
| Three redaction modes (REDACT/HASH/OMIT) (§3.1) | [internal/pii/engine.go](../internal/pii/engine.go) | `TestRedactForLogging_RedactMode`, `TestRedactForLogging_HashMode`, `TestRedactForLogging_OmitMode` in [internal/pii/engine_test.go](../internal/pii/engine_test.go) |
| Original response unchanged for client (§3.1, §3.5) | [internal/pii/engine.go](../internal/pii/engine.go) (`RedactForLogging` returns a copy), [internal/proxy/](../internal/proxy/), [internal/logging/](../internal/logging/) | `TestRedactForLogging_OriginalUnmodified` in [internal/pii/engine_test.go](../internal/pii/engine_test.go); `TestE2E_DPP_NoLeakOfBuyerEmail` in [test/e2e/dpp_test.go](../test/e2e/dpp_test.go) |
| Document-URL redaction (§3.5) | [internal/pii/registry.go](../internal/pii/registry.go) (reports/feeds/datakiosk rules) | `TestDocumentURLs_AreRedacted_Reports/Feeds/DataKiosk`, `TestDocumentURL_RedactionRoundTrip` in [internal/pii/registry_test.go](../internal/pii/registry_test.go) |
| Query-string redaction (§3.6) | [internal/pii/query.go](../internal/pii/query.go) | [internal/pii/query_test.go](../internal/pii/query_test.go); end-to-end coverage in [test/e2e/dpp_test.go](../test/e2e/dpp_test.go) |
| Cache exclusion on PII (§3.2) + `X-SP-Proxy-Cache: PII_EXCLUDED` header | [internal/cache/middleware.go](../internal/cache/middleware.go) | [internal/cache/middleware_test.go](../internal/cache/middleware_test.go), [test/e2e/cache_test.go](../test/e2e/cache_test.go) |
| FAIL_CLOSED default `true` (§3.7) | [internal/config/config.go](../internal/config/config.go), [internal/pii/registry.go](../internal/pii/registry.go) | `TestLoad_PIIFailClosedDefault`, `TestDefaults_FailClosedTrue` in [internal/config/config_test.go](../internal/config/config_test.go); `TestE2E_FailClosed_UnknownEndpointExcludedFromCache` in [test/e2e/failclosed_test.go](../test/e2e/failclosed_test.go) |
| 30-day body retention default (§3.3) | [internal/config/config.go](../internal/config/config.go), [internal/bodies/rotator.go](../internal/bodies/rotator.go) | `TestLoad_BodiesDefaults` in [internal/config/config_test.go](../internal/config/config_test.go) |
| 30-day metadata retention default (§3.3) | [internal/config/config.go](../internal/config/config.go), [internal/purge/purge.go](../internal/purge/purge.go) | `TestDefaults_MetadataRetention30Days` in [internal/config/config_test.go](../internal/config/config_test.go) |
| 13-month audit retention default (§3.3) | [internal/config/config.go](../internal/config/config.go), [internal/purge/purge.go](../internal/purge/purge.go) | `TestDefaults_AuditRetention13Months` in [internal/config/config_test.go](../internal/config/config_test.go) |
| Production-mode `dpp_compliance_warning` audit events emit (§4.2) | [internal/config/config.go](../internal/config/config.go) `Warnings()`; [cmd/smart-proxy/main.go](../cmd/smart-proxy/main.go) emission; event constant in [internal/audit/events.go](../internal/audit/events.go) | `TestProductionWarnings_*` family in [internal/config/config_test.go](../internal/config/config_test.go) |
| Dashboard loopback default (§4.2) | [internal/config/config.go](../internal/config/config.go), [internal/server/server.go](../internal/server/server.go) | `TestDefaults_DashboardBindAddrLoopback` in [internal/config/config_test.go](../internal/config/config_test.go); `TestDashboardBindAddr_DefaultsToLoopback` in [internal/server/server_test.go](../internal/server/server_test.go) |
| S3 server-side encryption enforcement (§4.3) | [internal/blob/s3.go](../internal/blob/s3.go) (sets `ServerSideEncryption` on every PutObject); validation in [internal/config/config.go](../internal/config/config.go) | `TestS3Backend_PutWithSSEAES256/SSEKMS/SSEKMSDSSE` in [internal/blob/s3_test.go](../internal/blob/s3_test.go); `TestLoad_S3SSE*` in [internal/config/config_test.go](../internal/config/config_test.go) |
| Plain-`http://` S3 endpoint refused in production (§4.3) | [internal/config/config.go](../internal/config/config.go) `Validate()` | `TestValidate_S3InsecureEndpointBlockedInProd`, `TestValidate_S3InsecureEndpointAllowedInDev` in [internal/config/config_test.go](../internal/config/config_test.go) |
| HTTPS to Amazon (DPP §1.5) | [internal/proxy/director.go](../internal/proxy/director.go) (forces `req.URL.Scheme = "https"`); region hosts in [internal/server/region.go](../internal/server/region.go) | `TestDirector_SetsSchemeAndHost` in [internal/proxy/director_test.go](../internal/proxy/director_test.go) |
| Merchant-scoped cache keys (AUP §4.4) | [internal/cache/keys.go](../internal/cache/keys.go), [internal/cache/middleware.go](../internal/cache/middleware.go) | [internal/cache/keys_test.go](../internal/cache/keys_test.go); end-to-end in [test/e2e/merchant_test.go](../test/e2e/merchant_test.go) |
| Tokens (LWA + RDT) in process memory only (§4.1) | [internal/rdt/](../internal/rdt/) (in-memory `map`, no persistence); no token writers in [internal/storage/](../internal/storage/) or [internal/bodies/](../internal/bodies/) | structural: verified by absence of token persistence; redaction of bearer tokens covered by header-redaction tests above |
| End-to-end PII no-leak | (cross-cutting) | [test/e2e/dpp_test.go](../test/e2e/dpp_test.go), [test/e2e/pii_test.go](../test/e2e/pii_test.go) |
