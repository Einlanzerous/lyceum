# Phase 2 (LYCM-200) — Web Reader: scope & handoff

The TypeScript + epub.js reading client that consumes the Phase 1 backend
(now built, on `main`). This is the handoff brief: decisions, the exact API
contract, the ticket DAG, and how to pick the work up.

## Decisions (locked)

| Area            | Decision                                                              |
|-----------------|-----------------------------------------------------------------------|
| Framework       | Vue 3 + Vite + TypeScript (strict), Pinia (state), vue-router         |
| Dev integration | Vite dev proxy forwards `/upload /library /sync /books` to `:8080`     |
| Prod serving    | Go embeds the built `web/dist` and serves it same-origin (LYCM-207)   |
| Auth            | None in Phase 2 (trusted LAN); auth is Phase 4 / LYCM-405             |

Vue keeps the stack consistent with the other house projects (argosy,
switchyard). Same-origin prod serving means there is no CORS surface to
maintain; dev gets the same effect via the proxy, so the client always calls
relative URLs.

## Backend API contract (authoritative — from `internal/api`)

Errors are **plain text** (Go `http.Error`), not JSON. The typed client must
read `res.text()` on non-2xx and wrap it.

| Method / path              | Request                                  | Success           | Notes |
|----------------------------|------------------------------------------|-------------------|-------|
| `POST /upload`             | multipart, field `file`                  | `201` book JSON   | duplicate -> `409` (text) |
| `GET /library`             | -                                        | `200` `Book[]`    | |
| `GET /books/{id}/cover`    | -                                        | `200` image       | immutable cache |
| `GET /books/{id}/file`     | -                                        | `200` epub+zip    | Range supported; `attachment` disposition |
| `PUT /sync`                | JSON position                            | `200` position    | CFI validated; LWW by `updated_at` |
| `GET /sync?book_id=&device_id=` | -                                   | `200` position / `404` | `404` = none; resume from start |

```ts
type Book = { id: number; title: string; author: string; cover_url: string; progress?: number /* 0..1 */ }
type Position = { book_id: number; device_id: string; cfi: string; progress: number; updated_at: string /* ISO */ }
```

Cross-device resume is already handled server-side: `GET /sync` with a
device that has no row of its own falls back to the latest position across all
devices, so a fresh device resumes where another left off.

## device_id

The client generates a `crypto.randomUUID()` once, persists it in
`localStorage`, and sends it on every `/sync` call. One helper (`getDeviceId()`)
owns this; everything else reads from it.

## Tickets

Epic: **LYCM-200** (`LYCM-2`). Phase 1 is closed and unblocks all of these.

| Label    | Key      | Title                                   | Depends on            |
|----------|----------|-----------------------------------------|-----------------------|
| LYCM-201 | LYCM-13  | Bootstrap Vue 3 + Vite + TS shell       | — (first, serial)     |
| LYCM-202 | LYCM-14  | Typed API client                        | 201                   |
| LYCM-203 | LYCM-15  | Library view (grid + upload)            | 202                   |
| LYCM-204 | LYCM-16  | epub.js reader (rendering)              | 202                   |
| LYCM-205 | LYCM-17  | Capture & restore CFI on page turn      | 204 (+ backend 107)   |
| LYCM-206 | LYCM-18  | Flush sync on unload / backgrounding    | 205                   |
| LYCM-207 | LYCM-28  | Serve built SPA from Go (embed.FS)      | 201 (backend)         |

### Kickoff order

1. Serial: **LYCM-201** (shell + proxy + tooling).
2. Then **LYCM-202** (client) — everything else needs it.
3. Fan out: **LYCM-203** (library) ‖ **LYCM-204** (reader) ‖ **LYCM-207** (Go embed; backend, independent of 203/204).
4. **LYCM-205** after 204, then **LYCM-206** after 205.

## Testing reality (read this before trusting green checks)

epub.js renders into an **iframe** and needs a real browser layout. It is **not**
meaningfully unit-testable under jsdom/vitest. Split the work:

- **vitest unit tests** for the pure logic: API client (incl. `404 -> null` and
  plain-text -> `ApiError`), `device_id` persistence, progress math, the relocate
  debounce, the unload-flush payload, library component with a mocked client.
- **Playwright headless smoke** (or a documented manual `/verify`) for the
  rendering/relocation/close-reopen paths in LYCM-204/205/206. A PR for those
  must state which was done. Do not fake a passing render unit test.

## Gotchas already caught

- **Unload flush is not `sendBeacon`.** `navigator.sendBeacon` only does POST,
  but `/sync` is PUT-only. Use `fetch(url, { method: 'PUT', keepalive: true })`
  (LYCM-206). A beacon would require a new POST alias on the backend.
- **Go embed + missing dist.** `//go:embed` fails to compile if `web/dist` is
  absent. LYCM-207 commits a placeholder `dist/index.html` and adds a Makefile
  `build-web` target so `go build` always works; the real bundle is built before
  release.

## Dev setup

```sh
# terminal 1 — backend (loads .env: DATABASE_URL on the shared Postgres)
set -a && . ./.env && set +a && make run        # :8080

# terminal 2 — frontend (proxies API calls to :8080)
cd web && npm install && npm run dev            # :5173
```

## Handoff: running it

Same shape as Phase 1: a sequential, build-gated workflow (201 -> 202 -> fan-out)
or hand the refined tickets to a developer. If a workflow builds it,
independently trouble-check the output — the rendering paths especially, since
their unit tests cannot prove the reader actually renders. See
[switchyard-phase1.md](switchyard-phase1.md) for the Phase 1 contract these
endpoints come from.
