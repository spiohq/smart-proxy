# Contributing to Smart Proxy

Thank you for your interest in contributing to Smart Proxy! This document covers everything you need to get started, from setting up your development environment to submitting a pull request.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How to Contribute](#how-to-contribute)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Coding Conventions](#coding-conventions)
- [Commit Message Format](#commit-message-format)
- [Pull Request Process](#pull-request-process)
- [Reporting Bugs](#reporting-bugs)
- [Requesting Features](#requesting-features)

---

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to uphold a welcoming, inclusive, and harassment-free environment for everyone.

---

## How to Contribute

There are many ways to contribute beyond writing code:

- **Report bugs.** Found something broken? [Open an issue](#reporting-bugs).
- **Suggest features.** Have an idea? [Start a discussion](#requesting-features).
- **Improve documentation.** Typos, unclear explanations, missing examples: all welcome.
- **Write tests.** Help us increase coverage, especially for edge cases in caching and rate limiting.
- **Review pull requests.** Fresh eyes catch things automated tools miss.
- **Share knowledge.** Write about your experience using Smart Proxy on [spiohq.com](https://spiohq.com) or your own blog.

---

## Development Setup

### Prerequisites

| Tool | Minimum Version | Purpose |
|---|---|---|
| **Go** | 1.25 | Backend compilation |
| **Node.js** | 20 | Dashboard SPA build |
| **npm** | 10 | Frontend dependency management |
| **Docker** | 20.10 | Containerized development and testing |
| **Make** | 3.81 | Build automation |
| **golangci-lint** | 1.62 | Go linting (install via [golangci-lint.run](https://golangci-lint.run/welcome/install/)) |

### Getting Started

```bash
# 1. Fork the repository on GitHub, then clone your fork
git clone https://github.com/YOUR_USERNAME/smart-proxy.git
cd smart-proxy

# 2. Add the upstream remote
git remote add upstream https://github.com/spiohq/smart-proxy.git

# 3. Install frontend dependencies
cd web && npm install && cd ..

# 4. Build and run locally
make build       # Builds the SvelteKit dashboard + Go binary
./bin/smart-proxy

# 5. Verify it works
curl -s http://localhost:8080/health
open http://localhost:9090   # Dashboard
```

### Docker-Based Development (recommended)

If you prefer a containerized workflow:

```bash
cd web && npm install && cd ..
make dev
```

This uses `docker-compose.dev.yml` to build from your local source and hot-reload on changes.

### Running Tests

```bash
make test       # Unit tests with race detection (-race)
make e2e-test   # End-to-end tests against a running proxy
make lint       # golangci-lint (must pass before PR merge)
```

All three must pass before a pull request can be merged.

---

## Project Structure

```
smart-proxy/
├── cmd/                    # Application entrypoint
│   └── smart-proxy/        # main.go
├── internal/               # Private application code (not importable)
│   ├── cache/              # LRU cache with 4-tier TTL, POST body caching, batch per-element caching
│   ├── config/             # Environment variable parsing
│   ├── dashboard/          # Embedded SvelteKit SPA (go:embed)
│   │   └── static/         # <- output of `make web-build`
│   ├── logging/            # Async request/response logger
│   ├── merchant/           # Merchant identity resolution
│   ├── prommetrics/        # Prometheus metrics collectors
│   ├── middleware/         # HTTP middleware chain
│   ├── pii/                # PII detection and redaction
│   ├── proxy/              # Reverse proxy to SP-API
│   ├── ratelimit/          # Token-bucket rate limiter
│   └── storage/            # SQLite + body file storage
├── web/                    # SvelteKit dashboard source
│   ├── src/
│   ├── package.json
│   └── svelte.config.js
├── docs/                   # Extended documentation
├── deploy/                 # Docker Compose files, example.env
├── e2e/                    # End-to-end test suite
├── Makefile
├── Dockerfile
└── docker-compose.yml
```

> [!NOTE]
> All Go application code lives under `internal/` and is not intended to be imported as a library. The `cmd/` package contains only the application entrypoint.

---

## Coding Conventions

### Go

- **Format with `gofmt`.** This is non-negotiable. All Go code must be formatted with `gofmt` (or `goimports`). CI will reject unformatted code.
- **Lint with `golangci-lint`.** Run `make lint` before committing. The project uses the configuration in `.golangci.yml`.
- **No CGO.** The project builds as a pure Go binary. Do not introduce CGO dependencies.
- **Error handling:** return errors rather than panicking. Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the error chain. Never silently discard errors.
- **Naming:** follow the conventions in [Effective Go](https://go.dev/doc/effective_go). Use `MixedCaps` for exported identifiers, `mixedCaps` for unexported. Avoid stuttering (`cache.CacheItem` -> `cache.Item`).
- **Package design:** keep packages focused. Each package in `internal/` should have a single clear responsibility. Avoid circular dependencies.
- **Tests:** place tests in the same package (`_test.go` files). Use table-driven tests where applicable. Name test functions `TestFunctionName_Scenario`. Include the `-race` flag (already in `make test`).
- **Comments:** all exported functions, types, and constants must have a doc comment starting with the identifier name. Non-obvious internal logic should have inline comments explaining *why*, not *what*.
- **Context:** pass `context.Context` as the first parameter where applicable, especially in middleware and proxy code.
- **Concurrency:** document goroutine ownership and lifecycle. Use channels or `sync` primitives; avoid shared mutable state without synchronization.

### SvelteKit / Frontend

- **Svelte 5 runes:** use the Svelte 5 runes API (`$state`, `$derived`, `$effect`) rather than the legacy reactive declarations.
- **TypeScript:** all new frontend code should be TypeScript (`.ts` / `.svelte` with `<script lang="ts">`).
- **TailwindCSS:** use Tailwind utility classes for styling. Avoid custom CSS unless Tailwind cannot express the design intent. Do not use `@apply` in component styles.
- **Components:** keep components small and focused. Extract reusable UI into `web/src/lib/components/`.
- **Formatting:** run `npm run format` (Prettier) before committing frontend code.
- **Linting:** run `npm run lint` (ESLint + svelte-check) before committing. CI enforces this.

### General

- **No new dependencies without discussion.** Open an issue before adding a new Go module or npm package. We keep the dependency tree minimal.
- **Feature flags:** new features behind environment variables when possible, so they can be tested without affecting existing behavior.
- **Backward compatibility:** changes to request/response headers, environment variables, or the configuration format must be backward-compatible or clearly documented as breaking changes.

---

## Commit Message Format

We follow [Conventional Commits](https://www.conventionalcommits.org/) with the following types:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

### Types

| Type | When to use |
|---|---|
| `feat` | A new feature or user-facing enhancement |
| `fix` | A bug fix |
| `perf` | A performance improvement |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `test` | Adding or updating tests |
| `docs` | Documentation-only changes |
| `build` | Changes to the build system, Makefile, Dockerfile, CI |
| `chore` | Dependency updates, tooling, config that doesn't affect users |

### Scopes

Use the package or area name as scope: `cache`, `ratelimit`, `proxy`, `pii`, `merchant`, `config`, `dashboard`, `metrics`, `logging`, `storage`, `docker`, `ci`, `docs`.

### Examples

```
feat(cache): add per-endpoint TTL override via config

Allow operators to define custom cache TTLs per SP-API endpoint
pattern in the environment config, overriding the default 60s TTL.

Closes #42
```

```
fix(ratelimit): prevent bucket leak on context cancellation

Token was consumed but not returned when the request context was
cancelled while waiting in the queue. Now the token is released
back to the bucket on cancellation.
```

```
docs(readme): add Mermaid architecture diagram
```

### Rules

- **Subject line:** imperative mood, lowercase, no period at the end, max 72 characters.
- **Body:** explain *what* and *why*, not *how*. Wrap at 80 characters.
- **Footer:** reference issues with `Closes #N` or `Refs #N`.
- **Breaking changes:** add `BREAKING CHANGE:` in the footer or `!` after the type: `feat(config)!: rename SP_PROXY_CACHE_TTL to SP_PROXY_CACHE_DEFAULT_TTL`.

---

## Pull Request Process

### Before You Start

1. **Check existing issues and PRs.** Someone might already be working on the same thing.
2. **Open an issue first for large changes.** Discuss the approach before investing significant time. Bug fixes and small improvements can go straight to a PR.
3. **Keep your fork up to date:**
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

### Creating a Pull Request

1. **Create a feature branch** from `main`:
   ```bash
   git checkout -b feat/my-feature
   ```

2. **Make your changes.** Follow the coding conventions and commit message format above.

3. **Run the full check suite locally:**
   ```bash
   make lint        # Go linting
   make test        # Go unit tests
   make e2e-test    # End-to-end tests
   make web-build   # Ensure frontend builds cleanly
   ```

4. **Push and open a PR** against `spiohq/smart-proxy:main`.

5. **Fill out the PR template.** Describe what you changed, why, and how to test it. Link the related issue if one exists.

### Review Criteria

Maintainers will review your PR for:

- **Correctness:** does it do what it claims? Are edge cases handled?
- **Tests:** does it include tests for new behavior? Do existing tests still pass?
- **Code quality:** does it follow the coding conventions? Is it readable and maintainable?
- **Performance:** could it introduce latency in the hot path (middleware pipeline)?
- **Backward compatibility:** does it break existing configurations, headers, or APIs?
- **Documentation:** are new features or changed behavior documented?

### After Review

- Address review feedback by pushing new commits (don't force-push during review, as it makes it harder to see what changed).
- Once approved, a maintainer will squash-merge your PR.
- Your contribution will be credited in the release notes.

---

## Reporting Bugs

[Open a bug report](https://github.com/spiohq/smart-proxy/issues/new?template=bug_report.md) with:

- **Smart Proxy version** (or commit hash)
- **How you're running it** (Docker, binary, source)
- **Steps to reproduce:** a minimal, reproducible example
- **Expected vs. actual behavior**
- **Relevant logs.** Redact any PII or access tokens before posting.

> [!WARNING]
> **Security vulnerabilities** should not be reported via GitHub Issues. See [SECURITY.md](SECURITY.md) for responsible disclosure instructions.

---

## Requesting Features

[Open a feature request](https://github.com/spiohq/smart-proxy/issues/new?template=feature_request.md) or start a discussion on [spiohq.com](https://spiohq.com). Include:

- **The problem you're solving.** What's the pain point?
- **Your proposed solution.** How should Smart Proxy handle it?
- **Alternatives you've considered.** What else could work?
- **Your use case.** How many sellers, which SP-API endpoints, what scale?

---

## Thank You

Every contribution makes Smart Proxy better for the entire SP-API developer community. Whether it's a typo fix or a major feature, it matters.