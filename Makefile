BINARY := lyceum
PKG := ./...
WEB_DIR := web

# Load .env (DATABASE_URL, TEST_DATABASE_URL) if present.
ifneq (,$(wildcard ./.env))
include .env
export
endif

.PHONY: build build-web release run dev check-env web-deps test lint vet tidy

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

# One-command dev environment: the Go backend (API + auto-migrate on boot, :8080)
# alongside the Vite dev server (HMR), which proxies API routes to the backend.
# Open the Vite URL it prints (default http://localhost:5173). Ctrl-C stops both.
dev: check-env web-deps
	@echo "==> lyceum backend (:8080) + vite dev server — Ctrl-C stops both"
	@go run ./cmd/lyceum & back=$$!; \
		( cd $(WEB_DIR) && npm run dev ) & front=$$!; \
		trap 'kill $$back $$front 2>/dev/null' INT TERM; \
		wait

# Guard: the backend needs .env for the Postgres DSN/password (loaded above).
check-env:
	@test -f .env || { \
		echo "No .env found. Create one and set the lyceum_user password:"; \
		echo "    cp .env.example .env"; \
		exit 1; \
	}

# Install web dependencies on first run (skipped once node_modules exists).
web-deps:
	@test -d $(WEB_DIR)/node_modules || { echo "==> installing web deps"; cd $(WEB_DIR) && npm install; }

test:
	go test $(PKG)

vet:
	go vet $(PKG)

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; ran go vet only"

tidy:
	go mod tidy
