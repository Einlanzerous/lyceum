BINARY := lyceum
PKG := ./...

# Load .env (DATABASE_URL, TEST_DATABASE_URL) if present.
ifneq (,$(wildcard ./.env))
include .env
export
endif

.PHONY: build run test lint vet tidy

build:
	go build -o bin/$(BINARY) ./cmd/lyceum

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
