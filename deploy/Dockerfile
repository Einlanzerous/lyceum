# syntax=docker/dockerfile:1

# Production image (LYCM-61): one self-contained Go binary that serves the JSON
# API *and* the embedded Vue reader SPA same-origin (web/embed.go). No separate
# frontend service, no CORS surface for the web build. construct-server pulls
# ghcr.io/einlanzerous/lyceum:latest. Build from the repo root:
#   docker build -f deploy/Dockerfile -t lyceum:latest .

# 1) Build the Vite SPA -> web/dist (embedded by the Go binary in step 2).
#    Regular `npm run build` = same-origin API (LYCM-207); the native/Wails
#    flavour is a different mode and not used here.
FROM node:20-bookworm-slim AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2) Build the Go binary with the SPA embedded. Copy the freshly built dist over
#    the checked-in placeholder before `go build` so //go:embed picks up the real
#    bundle. Pure Go (pgx, archive/zip) => CGO disabled for a static binary.
FROM golang:1.25-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/lyceum ./cmd/lyceum

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
