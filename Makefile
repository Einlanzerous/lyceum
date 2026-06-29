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

# One-command dev environment: the Go backend (API + auto-migrate on boot)
# alongside the Vite dev server (HMR), which proxies API routes to the backend.
# Vite runs with --host so it's reachable from another machine (this serves from
# a server, tested from a desktop); the backend stays on localhost since Vite
# proxies API calls to it server-side. Open the Network URL Vite prints; Ctrl-C
# stops both.
#
# Backend port: `make dev PORT=9000` wins, else LYCEUM_ADDR's port (.env), else
# 8080 — but if that port is already taken (e.g. another service on 8080) we fall
# back to a free one so the reader never proxies to the wrong service. Both the
# backend and Vite's proxy are pinned to the same chosen port.
dev: check-env web-deps
	@port="$(PORT)"; \
		[ -n "$$port" ] || port="$${LYCEUM_ADDR##*:}"; \
		[ -n "$$port" ] || port=8080; \
		if command -v python3 >/dev/null 2>&1 && \
		   ! python3 -c "import socket,sys; sys.exit(1 if socket.socket().connect_ex(('127.0.0.1',int('$$port')))==0 else 0)"; then \
			free=$$(python3 -c "import socket; s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1])"); \
			echo "==> port $$port is in use; using free backend port $$free instead"; \
			port=$$free; \
		fi; \
		echo "==> lyceum backend (:$$port) + vite dev server (--host) — open Vite's Network URL; Ctrl-C stops both"; \
		LYCEUM_ADDR=":$$port" go run ./cmd/lyceum & back=$$!; \
		( cd $(WEB_DIR) && LYCEUM_BACKEND="http://localhost:$$port" npm run dev -- --host ) & front=$$!; \
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
