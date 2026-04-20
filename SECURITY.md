# Security Policy

The Smart Proxy team takes security seriously. We appreciate responsible disclosure of vulnerabilities and will make every effort to acknowledge and address reports promptly.

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