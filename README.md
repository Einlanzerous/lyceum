# Lyceum

A cross-platform, DRM-free ebook reader and syncing ecosystem.

- **Backend** — Go + Postgres. EPUB ingestion/parsing, library management, reading-position sync via EPUB CFI.
- **Web reader** — TypeScript + [`epub.js`](https://github.com/futurepress/epub.js), bidirectional sync with the backend.
- **Wrappers** — Wails (Windows native), Capacitor (Android APK).
- **Ecosystem** — "Send to Kindle" SMTP delivery; API hooks for **Project Eidolon** (reading location + raw chapter text for local TTS streaming).

Tracked in Switchyard under project key **`LYCM`**.

## Layout

```
cmd/lyceum/      # server entrypoint (Phase 1)
internal/store/  # Postgres schema + repository (LYCM-102/103)
internal/epub/   # EPUB metadata parser + CFI utils (LYCM-104/108)
internal/api/    # REST handlers: /upload /library /sync (LYCM-105/106/107)
migrations/      # embedded SQL migrations (LYCM-102)
web/             # TypeScript + epub.js reader (Phase 2)
docs/            # architecture + eidolon-api contract
```

## Database

Lyceum uses the shared construct-server Postgres 16 (`postgres` container,
`127.0.0.1:5432`). The `lyceum` and `lyceum_test` databases and the
`lyceum_user` role are provisioned by `~/construct-server/db/init-db.sh`.

Connection is via `DATABASE_URL` (a libpq/pgx DSN). Copy `.env.example` to
`.env` and fill in the password (kept out of git):

```sh
cp .env.example .env     # then set the lyceum_user password
```

| Var                  | Purpose                                        |
|----------------------|------------------------------------------------|
| `DATABASE_URL`       | App connection (the `lyceum` database)         |
| `TEST_DATABASE_URL`  | Store/API tests (the `lyceum_test` database)   |

## Quickstart

```sh
cp .env.example .env           # then set the lyceum_user password
make dev                       # backend (:8080, auto-migrates) + Vite reader (HMR)
```

`make dev` runs both processes together and proxies the reader's API calls to
the backend; open the Vite URL it prints (default http://localhost:5173) and
Ctrl-C stops both. Backend only:

```sh
go build ./...                 # compile
set -a; . ./.env; set +a       # load DATABASE_URL
make run                       # boots HTTP server with /healthz
curl localhost:8080/healthz
```

## Ecosystem & Agent Integration (LYCM-400)

Phase 4 adds two integrations on top of the core, both gated by static bearer
tokens (`LYCEUM_API_TOKENS`, scopes `eidolon:read` / `delivery:send`). See
[`.env.example`](.env.example) for every knob.

- **Send to Kindle** (LYCM-401/402) — configure an SMTP relay
  (`LYCEUM_SMTP_*`) and a `LYCEUM_KINDLE_ADDR`; deliveries run off an in-process
  async queue.
  - `POST /books/{id}/send-to-kindle` *(scope `delivery:send`)* — body
    `{"to_addr": "..."}` optional, falls back to the configured address. Returns
    `202` with a queued delivery record.
  - `GET /books/{id}/deliveries` *(scope `delivery:send`)* — delivery history /
    status (`queued` → `sent` | `failed`).
  - With `LYCEUM_KINDLE_AUTO_SEND=true`, every uploaded book is auto-delivered.
- **Project Eidolon hooks** (LYCM-403/404) — read-only reading-location and
  TTS-ready chapter text under `/eidolon/*` *(scope `eidolon:read`)*. Contract:
  [`docs/eidolon-api.md`](docs/eidolon-api.md).

## Roadmap (Switchyard epics)

| Epic     | Phase | Theme                                   |
|----------|-------|-----------------------------------------|
| LYCM-100 | 1     | Core foundation (Go & storage)          |
| LYCM-200 | 2     | Web reader (TypeScript & epub.js)       |
| LYCM-300 | 3     | Cross-platform wrappers (Wails/Capacitor)|
| LYCM-400 | 4     | Ecosystem & agent integration           |
