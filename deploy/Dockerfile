# syntax=docker/dockerfile:1

# Production image (LYCM-61): one self-contained Go binary that serves the JSON
# API *and* the embedded Vue reader SPA same-origin (web/embed.go). No separate
# frontend service, no CORS surface for the web build. construct-server pulls
# ghcr.io/einlanzerous/lyceum:latest, which publish.yml publishes from the
# release tag build only — never from a main push, so the deployed image always
# carries a real semver to report on /healthz. Build from the repo root:
#   docker build -f deploy/Dockerfile -t lyceum:latest .

# 1) Build the Vite SPA -> web/dist (embedded by the Go binary in step 2).
#    Regular `bun run build` = same-origin API (LYCM-207); the native/Wails
#    flavour is a different mode and not used here.
#    Bun is pinned (LYCM-71): a floating "1-debian" can pull a newer bun whose
#    module resolution breaks vue-tsc on .vue imports. Keep in sync with
#    web/package.json's `packageManager` (the source of truth) and the
#    setup-bun steps in web.yml / wrappers.yml / windows-release.yml.
FROM oven/bun:1.3.14-debian AS web
WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# 2) Build the Go binary with the SPA embedded. Copy the freshly built dist over
#    the checked-in placeholder before `go build` so //go:embed picks up the real
#    bundle. Pure Go (pgx, archive/zip) => CGO disabled for a static binary.
FROM golang:1.25-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
#    Build identity (LYCM-121). Stamped into the binary and reported by
#    /healthz, which Switchyard's delivery reconciler polls to record what is
#    actually running (the SWY-192 contract, rolled out by SERV-128).
#
#    Both default to EMPTY, not to a placeholder like "docker". An ARG that is
#    declared but never passed expands to an empty string either way, so the
#    only question is what the binary does with it — and `internal/version`
#    maps empty back to "dev"/null. A "docker" default would instead put the
#    literal string "docker" in the delivery ledger where a version belongs.
#
#    There is deliberately no default of the release version: an image built
#    outside the publish workflow must not be able to claim it is a release.
#
#    VERSION is bare semver, no "v" prefix — it is compared with strict
#    equality against org.opencontainers.image.version, which metadata-action
#    stamps bare. GIT_SHA is the full 40-char commit; /healthz reports it
#    verbatim rather than abbreviating.
ARG VERSION=
ARG GIT_SHA=
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/magos/lyceum/internal/version.Version=${VERSION} \
      -X github.com/magos/lyceum/internal/version.Commit=${GIT_SHA}" \
    -o /out/lyceum ./cmd/lyceum

# 3) Runtime: slim Alpine with ca-certificates (HTTPS cover fetch + SMTP TLS) and
#    wget (busybox) for the compose healthcheck. Blobs live on a mounted volume
#    (LYCEUM_DATA_DIR); the books watch dir is mounted read-only at run time.
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/lyceum /usr/local/bin/lyceum
ENV LYCEUM_ADDR=":4005" \
    LYCEUM_DATA_DIR="/data/blobs"
EXPOSE 4005
ENTRYPOINT ["lyceum"]
