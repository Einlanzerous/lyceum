BINARY := lyceum
PKG := ./...
WEB_DIR := web

# Load .env (DATABASE_URL, TEST_DATABASE_URL) if present.
ifneq (,$(wildcard ./.env))
include .env
export
endif

.PHONY: build build-web release run test lint vet tidy

build:
	go build -o bin/$(BINARY) ./cmd/lyceum

# Build the Vite SPA into web/dist so //go:embed picks up the real bundle.
# `go build` alone still compiles (the placeholder web/dist/index.html is
# embedded); run this first whenever the binary must actually serve the reader.
build-web:
	cd $(WEB_DIR) && npm ci && npm run build

# Production single binary: real SPA bundle embedded into the Go server.
release: build-web build

run:
	go run ./cmd/lyceum

test:
	go test $(PKG)

vet:
	go vet $(PKG)

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; ran go vet only"

tidy:
	go mod tidy
