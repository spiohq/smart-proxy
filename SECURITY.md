# Security Policy

The Smart Proxy team takes security seriously. We appreciate responsible disclosure of vulnerabilities and will make every effort to acknowledge and address reports promptly.

---

## Deployment Requirements

Smart Proxy does **not** perform application-level encryption at rest. Operators MUST satisfy these requirements when handling production SP-API traffic:

### Encrypted storage volumes

The proxy persists two things to disk: a SQLite metadata DB (`SP_PROXY_SQLITE_PATH`, default `/data/sp-proxy.db`) and the active hour of body JSONL files (`SP_PROXY_BODIES_PATH/current/`, default `/data/bodies`). Bodies are PII-redacted before write (see [docs/DPP_COMPLIANCE.md](docs/DPP_COMPLIANCE.md)), but request metadata (paths, query strings, status codes, latencies) is stored in the clear.

Mount the proxy's data volume on encrypted block storage:

- **AWS:** EBS encryption (default for new volumes since 2023) or encrypted EFS.
- **GCP:** Persistent Disks are encrypted by default.
- **Azure:** Managed Disks with SSE.
- **Self-hosted / bare metal:** LUKS, dm-crypt, or ZFS native encryption.
- **Kubernetes:** Use a StorageClass backed by an encrypted CSI driver.

Tokens (LWA + RDT) live in **process memory only** and never hit disk. Restarting the proxy drops the cache cold.

### Object storage encryption

When `SP_PROXY_BODIES_BACKEND=s3`, set `SP_PROXY_S3_SSE` to enforce server-side encryption on every PutObject (see [docs/STORAGE.md](docs/STORAGE.md#server-side-encryption)). Pair with a bucket policy that denies unencrypted uploads.

**Application-level encryption is intentionally not implemented.** Operators provide at-rest protection via volume encryption (EBS, LUKS, dm-crypt) and S3 server-side encryption (`SP_PROXY_S3_SSE`).

### S3 backend credentials

If you configure `SP_PROXY_BODIES_BACKEND=s3` with a long-lived IAM user key (`AKIA...` access-key-ID prefix), the proxy emits a `dpp_compliance_warning` audit event on every startup until you migrate to STS-issued credentials AND set `SP_PROXY_S3_SSE=AES256` (or `aws:kms`). Long-lived static keys plus plaintext (no-SSE) buckets compound the blast radius of a host compromise; rotate to short-lived role-assumed credentials and enforce server-side encryption.

The proxy itself never persists `SP_PROXY_S3_ACCESS_KEY` -- it lives in process memory only and is passed to the AWS SDK. The warning is operator-help to nudge a compounding-risk configuration toward the safer pattern.

### Prometheus metrics endpoint

By default `/metrics` is mounted on the dashboard port (loopback-only when `SP_PROXY_DASHBOARD_BIND_ADDR=127.0.0.1`). If you front the dashboard with an authenticating reverse proxy, decide explicitly whether scraping should share that auth boundary:

- **Easiest:** keep `SP_PROXY_PROMETHEUS_PORT=0` (default) and let your reverse-proxy / scraper auth at the same gate as the dashboard.
- **Cleaner separation:** set `SP_PROXY_PROMETHEUS_PORT=9091` (or any other port). Scraping then has its own listener you can firewall independently of the dashboard.

Prometheus labels include `merchant`, so a scrape job exposed beyond the operator's network would leak per-tenant traffic shape. Keep `/metrics` on a network where only the metrics collector can reach it.

### Validation bypass header

When `SP_PROXY_VALIDATION_ENABLED=true`, the `X-SP-Proxy-Skip-Validation: true` request header bypasses proxy-side OpenAPI validation for a single request. This is intended for trusted internal callers that have already validated their payload, or for rollout scenarios where a specific endpoint is known to behave outside the published spec.

**Only allow this header from trusted callers.** Because Smart Proxy has no built-in authentication, any client that can reach the proxy port can set this header and bypass validation entirely. If you rely on validation as a guardrail against buggy clients, enforce at the network level that untrusted callers cannot set `X-SP-Proxy-Skip-Validation`. An upstream authenticating reverse proxy can strip the header before forwarding to the proxy.

### Network exposure

Smart Proxy is designed to run as a **sidecar or private-network component**. It MUST NOT be exposed directly to the public internet without an authenticating reverse proxy in front of it. The proxy honors `X-SP-Proxy-Merchant-Id` for tenant identification; an unauthenticated public endpoint would let any caller self-claim any merchant key.

Recommended deployment shapes:

- Same-host sidecar (loopback only).
- Private VPC subnet, accessed by your application via an internal load balancer.
- Behind an authenticating reverse proxy (mTLS, OAuth, IP allowlist).

The dashboard (`SP_PROXY_PORT_DASHBOARD`, default `9090`) ships **without authentication**. It MUST be bound to a private interface or sit behind an auth layer; never expose it to the internet.

The dashboard now defaults to bind address `127.0.0.1` (`SP_PROXY_DASHBOARD_BIND_ADDR`). For container deployments, use a host-side `127.0.0.1:9090:9090` port mapping and set `SP_PROXY_DASHBOARD_BIND_ADDR=0.0.0.0` inside the container. The container will emit a `dpp_compliance_warning` audit event indicating that an authenticating reverse proxy must front the host port; this is the expected audit signal.

The region (data-plane) listeners now default to bind address `127.0.0.1` (`SP_PROXY_REGION_BIND_ADDR`) for the same reason. **Upgrade note for non-compose deployments:** the previous binary listened on `0.0.0.0` for the region ports unconditionally. Operators upgrading from a pre-F-01 release who reach the region ports via a LAN, VPN, or external load balancer must explicitly opt in by setting `SP_PROXY_REGION_BIND_ADDR=0.0.0.0` (or a specific interface address). The reference `docker-compose.yml` already sets `SP_PROXY_REGION_BIND_ADDR=0.0.0.0` inside the container alongside host-side `127.0.0.1:port` mappings; deployments based on it are unaffected.

### Migration 007 rollback note

The 0.x release that lands the F-02 work introduces a SQLite migration (`007_pii_redacted_split.sql`) that splits the legacy `pii_redacted` column into `pii_redacted_request` + `pii_redacted_response`. The new binary writes only the new columns and leaves `pii_redacted` static at its backfilled value. **An operator who rolls back to a pre-F-02 binary after migration 007 has been applied will see `piiRedacted=false` in the dashboard for any rows written by the new binary**, regardless of whether the response was actually redacted. The data on disk is correct (the new columns hold the truth) -- only the pre-F-02 binary cannot see it. If a rollback is necessary, the safe window is: roll back before any meaningful traffic has been processed by the new binary.

For the full DPP/AUP compliance reference -- shared-responsibility matrix, audit-prep checklist, and operator-specific touch points -- see [docs/DPP_COMPLIANCE.md](docs/DPP_COMPLIANCE.md).

---

## Reporting a Vulnerability

**Do not open public GitHub issues for security vulnerabilities.**

Instead, please report vulnerabilities via email:

**[proxysec@spiohq.com](mailto:proxysec@spiohq.com?subject=Security%20Vulnerability%20Report%3A%20Smart%20Proxy)**

Include the following in your report:

- **Description** of the vulnerability and its potential impact.
- **Steps to reproduce:** a minimal, reproducible example or proof of concept.
- **Affected component:** which package, middleware, or feature is involved (e.g., `internal/pii`, `internal/cache`, dashboard).
- **Smart Proxy version** or commit hash where you observed the issue.
- **Your suggested fix**, if you have one (optional but appreciated).

---

## Response Timeline

| Step | Target |
|---|---|
| Acknowledgment of your report | **48 hours** |
| Initial assessment and severity classification | **5 business days** |
| Fix developed and tested | **30 days** (critical/high severity) |
| Security advisory and patched release published | Coordinated with reporter |

We will keep you informed throughout the process. If you haven't received an acknowledgment within 48 hours, please follow up as your email may not have reached us.

---

## Scope

The following are **in scope** for security reports:

- **Proxy bypass:** requests that circumvent rate limiting, caching, PII redaction, or OpenAPI validation when they shouldn't.
- **PII leakage:** personally identifiable information appearing in logs, cache, metrics, or the dashboard when redaction is enabled.
- **Authentication/authorization issues:** unauthorized access to the dashboard, metrics endpoint, or stored request data.
- **Injection attacks:** SQL injection in SQLite queries, header injection, path traversal, or template injection in the dashboard.
- **Denial of service:** resource exhaustion via crafted requests (memory, CPU, disk, file descriptors) that bypass normal rate limiting.
- **Dependency vulnerabilities:** known CVEs in Go modules or npm packages used by the project.
- **Docker image issues:** insecure defaults, unnecessary privileges, or exposed secrets in the container image.
- **Information disclosure:** unintended exposure of internal state, configuration, or environment variables.

The following are **out of scope**:

- Vulnerabilities in Amazon SP-API itself. Report those to [Amazon](https://developer-docs.amazon.com/sp-api/).
- Issues that require physical access to the host machine.
- Social engineering attacks against maintainers or users.
- Denial of service via high-volume traffic that would overwhelm any service (volumetric DDoS).
- Reports from automated scanners without a demonstrated exploit or real-world impact.

---

## Disclosure Policy

We follow **coordinated disclosure**:

1. You report the vulnerability privately to us.
2. We acknowledge, assess, and develop a fix.
3. We coordinate a disclosure date with you (typically when the patched release is published).
4. We publish a [GitHub Security Advisory](https://github.com/spiohq/smart-proxy/security/advisories) with full details and credit to the reporter.

We ask that you:

- **Give us reasonable time** to address the issue before any public disclosure (minimum 90 days for non-critical issues).
- **Do not access, modify, or delete data** belonging to other users during your research.
- **Act in good faith.** Make a best effort to avoid disruption to the project and its users.

We commit to:

- **Not pursuing legal action** against researchers who follow this policy.
- **Crediting you** in the security advisory and release notes (unless you prefer to remain anonymous).
- **Being transparent** about the issue and the fix once a patch is available.

---

## Supported Versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Previous minor release | Security fixes only |
| Older versions | No |

We recommend always running the latest release. Security fixes are not backported beyond one minor version.

---

## security.txt

This project follows the [security.txt](https://securitytxt.org/) standard. The `/.well-known/security.txt` file on spiohq.com points to this policy.

---

Thank you for helping keep Smart Proxy and the SP-API developer community safe.