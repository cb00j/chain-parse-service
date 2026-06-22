.PHONY: build-parser build-api build-all test test-race test-cover test-short \
       lint fmt vet clean run-parser run-api \
       docker-build docker-build-parser docker-build-api \
       docker-up docker-down docker-ps docker-logs \
       build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-cross \
       help

# ─── Variables ───────────────────────────────────────────────────────────────

BINARY_DIR  := bin
PARSER_BIN  := $(BINARY_DIR)/parser
API_BIN     := $(BINARY_DIR)/api
MODULE      := $(shell head -1 go.mod | awk '{print $$2}')
DOCKER_DIR  := docker

# Version info injected via ldflags
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -s -w \
              -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT) \
              -X main.buildTime=$(BUILD_TIME)

# Docker
DOCKER_REGISTRY ?=
IMAGE_PREFIX    ?= chain-parse
PARSER_IMAGE    := $(if $(DOCKER_REGISTRY),$(DOCKER_REGISTRY)/)$(IMAGE_PREFIX)-parser
API_IMAGE       := $(if $(DOCKER_REGISTRY),$(DOCKER_REGISTRY)/)$(IMAGE_PREFIX)-api

# ─── Build ───────────────────────────────────────────────────────────────────

build-parser:
	@echo "==> Building parser..."
	@mkdir -p $(BINARY_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(PARSER_BIN) ./cmd/parser/

build-api:
	@echo "==> Building api..."
	@mkdir -p $(BINARY_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(API_BIN) ./cmd/api/

build-all: build-parser build-api

# ─── Cross-platform Build ────────────────────────────────────────────────────

build-linux-amd64:
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/parser-linux-amd64 ./cmd/parser/
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/api-linux-amd64 ./cmd/api/

build-linux-arm64:
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/parser-linux-arm64 ./cmd/parser/
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/api-linux-arm64 ./cmd/api/

build-darwin-amd64:
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/parser-darwin-amd64 ./cmd/parser/
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/api-darwin-amd64 ./cmd/api/

build-darwin-arm64:
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/parser-darwin-arm64 ./cmd/parser/
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/api-darwin-arm64 ./cmd/api/

build-cross: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "==> Cross-platform binaries in $(BINARY_DIR)/"

# ─── Test ────────────────────────────────────────────────────────────────────

test:
	go test ./...

test-race:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo "==> HTML coverage report: coverage.html"
	go tool cover -html=coverage.out -o coverage.html

test-short:
	go test -short ./...

# ─── Code Quality ───────────────────────────────────────────────────────────

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

vet:
	go vet ./...

# ─── Run ─────────────────────────────────────────────────────────────────────

run-parser: build-parser
	@[ -n "$(CHAIN)" ] || { echo "Usage: make run-parser CHAIN=bsc"; exit 1; }
	./$(PARSER_BIN) -chain $(CHAIN)

run-api: build-api
	./$(API_BIN)

# ─── Docker ──────────────────────────────────────────────────────────────────

docker-build: docker-build-parser docker-build-api

docker-build-parser:
	docker build -f $(DOCKER_DIR)/Dockerfile \
		--build-arg SERVICE=parser \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(PARSER_IMAGE):$(VERSION) \
		-t $(PARSER_IMAGE):latest \
		.

docker-build-api:
	docker build -f $(DOCKER_DIR)/Dockerfile \
		--build-arg SERVICE=api \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(API_IMAGE):$(VERSION) \
		-t $(API_IMAGE):latest \
		.

docker-up:
	docker compose -f $(DOCKER_DIR)/docker-compose.yml up -d

docker-down:
	docker compose -f $(DOCKER_DIR)/docker-compose.yml down

docker-ps:
	docker compose -f $(DOCKER_DIR)/docker-compose.yml ps

docker-logs:
	docker compose -f $(DOCKER_DIR)/docker-compose.yml logs -f

# ─── Clean ───────────────────────────────────────────────────────────────────

clean:
	rm -rf $(BINARY_DIR) coverage.out coverage.html

# ─── Help ────────────────────────────────────────────────────────────────────

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Build:"
	@echo "  build-parser        Build parser binary"
	@echo "  build-api           Build api binary"
	@echo "  build-all           Build all binaries"
	@echo "  build-cross         Build for all platforms (linux/darwin x amd64/arm64)"
	@echo ""
	@echo "Test:"
	@echo "  test                Run tests"
	@echo "  test-race           Run tests with race detector"
	@echo "  test-cover          Run tests with coverage report"
	@echo "  test-short          Run short tests only"
	@echo ""
	@echo "Code Quality:"
	@echo "  lint                Run golangci-lint"
	@echo "  fmt                 Format code (gofmt + goimports)"
	@echo "  vet                 Run go vet"
	@echo ""
	@echo "Run:"
	@echo "  run-parser CHAIN=x  Build and run parser (bsc/ethereum/solana/sui)"
	@echo "  run-api             Build and run api"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build        Build all Docker images"
	@echo "  docker-up           Start all services (docker compose)"
	@echo "  docker-down         Stop all services"
	@echo "  docker-ps           Show running services"
	@echo "  docker-logs         Tail logs from all services"
	@echo ""
	@echo "Other:"
	@echo "  clean               Remove build artifacts"
	@echo "  help                Show this help"
