.PHONY: build test e2e-test docker-build docker-run dev web-build clean quick-start

# Build the Go binary (requires dashboard SPA: make web-build)
build: web-build
	go build -o bin/smart-proxy ./cmd/smart-proxy/

# Run unit tests with race detection
test:
	go test ./... -race -count=1

# Run E2E tests (requires build tag)
e2e-test:
	go test -tags e2e ./test/e2e/ -timeout 60s -count=1

# Build Docker image
docker-build:
	docker build -t smart-proxy .

# Start with Docker Compose
docker-run:
	docker compose up

# Build from source and run locally (for development)
dev:
	make web-build
	docker compose -f docker-compose.dev.yml up --build

# Build the Svelte dashboard SPA
web-build:
	cd web && npm run build

# Remove build artifacts
clean:
	rm -rf bin/ dist/
	rm -f smart-proxy

# Build and run locally with Docker (quick start for first-time testing)
quick-start: docker-build
	@docker rm -f smart-proxy 2>/dev/null || true
	docker run -d --name smart-proxy -p 8080:8080 -p 9090:9090 -v sp-proxy-data:/data smart-proxy
	@echo ""
	@echo "smart-proxy is running!"
	@echo "  EU proxy:  http://localhost:8080"
	@echo "  Dashboard: http://localhost:9090"
	@echo ""
	@echo "Stop with: docker rm -f smart-proxy"
