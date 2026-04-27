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

### Network exposure

Smart Proxy is designed to run as a **sidecar or private-network component**. It MUST NOT be exposed directly to the public internet without an authenticating reverse proxy in front of it. The proxy honors `X-SP-Proxy-Merchant-Id` for tenant identification; an unauthenticated public endpoint would let any caller self-claim any merchant key.

Recommended deployment shapes:

- Same-host sidecar (loopback only).
- Private VPC subnet, accessed by your application via an internal load balancer.
- Behind an authenticating reverse proxy (mTLS, OAuth, IP allowlist).

The dashboard (`SP_PROXY_PORT_DASHBOARD`, default `9090`) ships **without authentication**. It MUST be bound to a private interface or sit behind an auth layer; never expose it to the internet.

The dashboard now defaults to bind address `127.0.0.1` (`SP_PROXY_DASHBOARD_BIND_ADDR`). For container deployments, use a host-side `127.0.0.1:9090:9090` port mapping and set `SP_PROXY_DASHBOARD_BIND_ADDR=0.0.0.0` inside the container. The container will emit a `dpp_compliance_warning` audit event indicating that an authenticating reverse proxy must front the host port; this is the expected audit signal.

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

- **Proxy bypass:** requests that circumvent rate limiting, caching, or PII redaction when they shouldn't.
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