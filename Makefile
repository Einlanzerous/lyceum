BINARY := lyceum
PKG := ./...
WEB_DIR := web

# Load .env (DATABASE_URL, TEST_DATABASE_URL) if present.
ifneq (,$(wildcard ./.env))
include .env
export
endif

.PHONY: build build-web build-web-native release run dev check-env web-deps test lint vet tidy wails-windows

build:
	go build -o bin/$(BINARY) ./cmd/lyceum

# Build the Vite SPA into web/dist so //go:embed picks up the real bundle.
# `go build` alone still compiles (the placeholder web/dist/index.html is
# embedded); run this first whenever the binary must actually serve the reader.
build-web:
	cd $(WEB_DIR) && npm ci && npm run build

# Build the SPA in *native* mode (API calls target a configured remote backend
# instead of same-origin). Consumed by the Wails desktop wrapper (LYCM-300).
build-web-native:
	cd $(WEB_DIR) && npm ci && npm run build:native

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
# 8080. The port is FIXED: if it's already in use we abort with guidance rather
# than silently falling back to a random free one. A random port is ephemeral —
# once the session ends it's dead, but a mobile/desktop client that saved that
# dev URL keeps dialing it forever and hangs on a blank library (LYCM-54). Set a
# stable LYCEUM_ADDR in .env (or pass PORT=) so the dev URL never changes between
# sessions. Both the backend and Vite's proxy are pinned to the chosen port.
dev: check-env web-deps
	@port="$(PORT)"; \
		[ -n "$$port" ] || port="$${LYCEUM_ADDR##*:}"; \
		[ -n "$$port" ] || port=8080; \
		if command -v python3 >/dev/null 2>&1 && \
		   python3 -c "import socket,sys; sys.exit(0 if socket.socket().connect_ex(('127.0.0.1',int('$$port')))==0 else 1)"; then \
			echo "==> ERROR: backend port $$port is already in use." >&2; \
			echo "    Lyceum's dev server uses a FIXED port so a saved app server-URL stays valid across sessions (LYCM-54)." >&2; \
			echo "    Free that port, or pick another: make dev PORT=NNNN" >&2; \
			echo "    (set LYCEUM_ADDR=:NNNN in .env to make it stick, and update the app's server URL to match)." >&2; \
			exit 1; \
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

# --- Cross-platform wrappers (LYCM-300) ---
# The Wails desktop shell needs its own toolchain (the Wails CLI); see
# wrappers/wails/README.md. It rebuilds the SPA in native mode as part of its
# build, so a stale web/dist won't leak in. (The Android app is a native Flutter
# project under mobile/ — see mobile/lyceum/README.md — not a make target here.)

# Windows .exe via Wails → wrappers/wails/build/bin/Lyceum.exe. Requires the
# Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1`).
# -skipbindings: the app exposes no bound Go methods, and binding generation
# runs a compiled probe binary — which can't execute when cross-compiling a
# Windows target from Linux. Safe to skip here and on a Windows host alike.
wails-windows:
	cd wrappers/wails && wails build -platform windows/amd64 -skipbindings
