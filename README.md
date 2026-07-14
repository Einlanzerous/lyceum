# Lyceum

A cross-platform, DRM-free ebook reader and syncing ecosystem.

- **Backend** — Go + Postgres. EPUB ingestion/parsing, library management, reading-position sync via EPUB CFI.
- **Web reader** — TypeScript + [`epub.js`](https://github.com/futurepress/epub.js), bidirectional sync with the backend.
- **Wrappers** — Wails (Windows native desktop).
- **Mobile** — native Flutter Android app (`mobile/`).
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
wrappers/        # native desktop shell: Wails (Windows) (Phase 3)
mobile/          # native Flutter Android app (LYCM-700)
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

## Native desktop wrapper (LYCM-300)

Phase 3 packages the *same* web reader as a native Windows `.exe` (Wails) that
reaches a remote Lyceum server and syncs just like the browser. See
[`wrappers/`](wrappers/) for the full picture; the short version:

- The shell embeds the SPA built with `npm run build:native`. In that mode the
  frontend prefixes every API call with a **server URL the user configures on
  first run** (Settings → Connection) instead of using same-origin relative
  URLs. The web build is unchanged.
- The backend allows the shell's cross-origin calls via a CORS allowlist
  (`internal/api/cors.go`). The built-in Wails origin works out of the box;
  `LYCEUM_CORS_ORIGINS` extends it.

```sh
make wails-windows     # → wrappers/wails/build/bin/Lyceum.exe   (needs the Wails CLI)
```

Android is a separate native Flutter app under [`mobile/`](mobile/lyceum)
(LYCM-700), not a web-shell wrapper.

## Accounts (LYCM-801)

Lyceum supports a household: several people share one shelf but keep their own
reading positions, so one person finishing a book doesn't show everyone else as
finished. There are no passwords — sign-in is by one-time invite.

**Enforcement is off by default** (`LYCEUM_AUTH=false`). While it is off the
reader core is open and every request is served as the owner, exactly as before
accounts existed — the clients don't send credentials yet. Turn it on once they
ship a sign-in screen.

- **The owner** is the account that adopts all pre-accounts reading history, and
  the only one who can invite or remove people. Set `LYCEUM_OWNER_EMAIL` /
  `LYCEUM_OWNER_NAME`; the row itself is seeded by migration 0011.
- **First boot** prints a one-time sign-in invite for the owner. Redeem it once
  (`POST /auth/session`) to get the long-lived session token the client carries as
  `Authorization: Bearer`. `lyceum mint-token` issues another if it's lost.
- **Adding someone** — `POST /admin/users {email, display_name}` returns *their*
  one-time invite. (This is also the hook Purser provisions through.) Invites
  expire after 7 days. The `/admin` routes are refused entirely while
  `LYCEUM_AUTH=false`, since a server that can't tell who is asking shouldn't be
  minting credentials — use `lyceum mint-token` on the host to bootstrap.
- **Two ways to present a session** — `Authorization: Bearer <token>` for native
  clients, or the `lyceum_session` cookie that sign-in also sets. The cookie is
  not optional garnish: the shelf loads covers with plain `<img src>` tags, which
  cannot carry a header.

Two token namespaces exist and are never interchangeable: **session tokens** (in
the database, guard the reader core, belong to people) and the **service tokens**
below (`LYCEUM_API_TOKENS`, scoped, belong to programs). A service token cannot
read a library; a session cannot drive a Kindle delivery.

| Route | |
|---|---|
| `POST /auth/session` | redeem an invite → `{user, session_token}` |
| `DELETE /auth/session` | sign out this device only |
| `GET` / `PATCH /auth/me` | current account; rename yourself |
| `POST` / `GET /admin/users` | invite a member (returns a one-time token) / list |
| `POST /admin/users/{id}/invite` | re-invite (a second device, or a lost token) |
| `DELETE /admin/users/{id}` | remove a member (the owner can't be removed) |

Reading positions are keyed `(book, user, device)`, so two people can read the
same book on the same shared tablet without colliding.

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

## Acquisition & inventory (LYCM-601)

Lyceum tracks **ownership** of a title separately from having its EPUB. The
`inventory` table is keyed by canonical ISBN-13 and carries an
`acquisition_state` (`owned` → `wanted` → `acquiring` → `ingested`) plus a
nullable link to the ingested `books` row — so a physical book you own can be
recorded before any digital file exists.

- **Capture** *(feeds LYCM-602 barcode scanning)*
  - `POST /inventory` — body `{"isbn": "...", "title"?, "author"?, "find_digital"?}`.
    Accepts any ISBN-10/13 form (hyphenated, `urn:isbn:`, ISBN-10), normalizes to
    ISBN-13, and records the title as `owned`. With `find_digital: true` it hands
    the ISBN to the acquisition pipeline and marks it `wanted` (a title already
    `ingested` is left untouched). Returns the inventory entry.
  - `GET /inventory` — all entries, most-recently-updated first.
- **Folder ingest** — set `LYCEUM_BOOKS_WATCH_DIR` to the acquisition stack's
  book library (`/data/media/books` in
  [`argosy-acquisition`](../../construct-server/services/argosy-acquisition)).
  A poller picks up new EPUBs and runs the *same* core as `POST /upload`
  (validate → SHA-256 dedup → store), then stamps the ISBN from the EPUB's
  `dc:identifier` onto inventory and flips it to `ingested`. Upload and folder
  ingest share one code path (`ingestEPUB`), so dedup holds across both.
- **Acquire** — the `argosy-acquisition` **Bindery** container grabs DRM-free
  EPUBs for owned titles and imports them to `/data/media/books`, where the
  watcher ingests them. The `ISBN → request a grab` step is behind an `Acquirer`
  seam (logging no-op by default); a live Bindery client is wired via
  `WithAcquirer` once that stack is configured.

## Roadmap (Switchyard epics)

| Epic     | Phase | Theme                                   |
|----------|-------|-----------------------------------------|
| LYCM-100 | 1     | Core foundation (Go & storage)          |
| LYCM-200 | 2     | Web reader (TypeScript & epub.js)       |
| LYCM-300 | 3     | Cross-platform wrappers (Wails desktop)  |
| LYCM-400 | 4     | Ecosystem & agent integration           |
