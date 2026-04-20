# ---- SPA build stage ----
FROM node:22-alpine AS web

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ---- Go build stage ----
FROM golang:1.25-alpine AS build

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
FROM alpine:3.21

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
