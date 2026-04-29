# ---- SPA build stage ----
# Base images pinned to digests to defend against tag-based supply-chain
# drift (F-20). Tag-only pulls let an upstream tag refresh silently change
# the runtime image; pair these pins with Renovate / Dependabot to bump
# digests on a schedule so security patches still flow. Verify each digest
# with `docker pull node:22-alpine && docker inspect node:22-alpine --format
# '{{index .RepoDigests 0}}'` before bumping.
FROM node:22-alpine@sha256:8ea2348b068a9544dae7317b4f3aafcdc032df1647bb7d768a05a5cad1a7683f AS web

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ---- Go build stage ----
FROM golang:1.25-alpine@sha256:5caaf1cca9dc351e13deafbc3879fd4754801acba8653fa9540cea125d01a71f AS build

WORKDIR /src

# Copy dependency files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY internal/ internal/

# Copy SPA build output into embedded assets directory
COPY --from=web /internal/dashboard/static/ internal/dashboard/static/

# Build static binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/smart-proxy ./cmd/smart-proxy/

# ---- Runtime stage ----
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

# Install CA certificates for HTTPS to SP-API endpoints
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -u 10001 -h /nonexistent -s /sbin/nologin proxy

# Create data directory for SQLite DB and body files
RUN mkdir -p /data && chown proxy:proxy /data

# Copy binary from build stage
COPY --from=build /usr/local/bin/smart-proxy /usr/local/bin/smart-proxy

EXPOSE 8080 8081 8082 9090

USER proxy

ENTRYPOINT ["/usr/local/bin/smart-proxy"]
