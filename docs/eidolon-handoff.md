# Handoff: integrating Project Eidolon with Lyceum (LYCM-400)

Drop-in brief for the **Eidolon (EIDO)** side. Lyceum's Phase-4 ecosystem work
(epic LYCM-400) is done and verified; these are the hooks Eidolon consumes for
local TTS streaming. Canonical wire contract: [`eidolon-api.md`](./eidolon-api.md).

## What Lyceum exposes

Two read-only endpoints, both requiring a bearer token with scope `eidolon:read`:

- `GET /eidolon/books/{id}/location` → `{cfi, progress, chapter_href, updated_at}`
  — the reader's current position (latest synced across devices).
- `GET /eidolon/books/{id}/chapter?index=|href=|from_cfi=` → `text/plain`
  — TTS-ready chapter text: HTML stripped, paragraphs blank-line separated,
  `script`/`style` dropped, whitespace collapsed, streamed.

Discovery of book ids uses the existing **unauthenticated** `GET /library`
(`[{id, title, author, cover_url, progress}]`).

## Connection + auth

- **Base URL:** the Lyceum server on the home box — `http://<lyceum-host>:8080`
  by default (`LYCEUM_ADDR`). Eidolon runs locally and reaches it over the LAN.
- **Token:** the Lyceum operator provisions one and shares the string
  out-of-band:
  ```sh
  # on the Lyceum server, e.g. in .env
  LYCEUM_API_TOKENS=eidolon-tts=eidolon:read
  ```
  Then every Eidolon request sends `Authorization: Bearer eidolon-tts`.
- **Fail-closed:** with no token configured, or a missing/unknown token, every
  `/eidolon/*` request is `401`; a valid token without `eidolon:read` is `403`.

## The TTS loop (recommended flow)

1. **Pick a book** — `GET /library`, choose an `id`.
2. **Get where the reader is** — `GET /eidolon/books/{id}/location`
   → note `cfi` and `chapter_href`. (`404` = the book has no synced position yet.)
3. **Fetch the chapter to speak** — `GET /eidolon/books/{id}/chapter?from_cfi=<cfi>`
   (resolves to the chapter that CFI lives in and streams it), or
   `?href=<chapter_href>`, or `?index=N`.
4. **Speak it**, then **advance**: walk spine indices (`?index=N+1`, `N+2`, …)
   until `404` ("spine index out of range") marks the end of the book.

```sh
B=http://lyceum.lan:8080
H='Authorization: Bearer eidolon-tts'
curl -s "$B/library"                                          # → find id (e.g. 8)
curl -s -H "$H" "$B/eidolon/books/8/location"                 # → {cfi, chapter_href, …}
curl -s -H "$H" "$B/eidolon/books/8/chapter?from_cfi=epubcfi(/6/8!/4/2/1:0)"
curl -s -H "$H" "$B/eidolon/books/8/chapter?index=5"          # by spine index
```

## Caveats Eidolon must handle

- **`from_cfi` is chapter-granular.** It selects the chapter the CFI is in and
  streams from the chapter *start* — there is no sub-paragraph offset yet. If
  Eidolon needs to resume mid-chapter, track its own character/paragraph offset
  into the returned text.
- **Empty chapters are normal.** Cover, image, and section-break spine items
  return an empty body (no extractable text). Skip empties; iterate `index` to
  find prose. (For the Eisenhorn omnibus, indices 0/1/3 are empty front-matter;
  prose starts around index 5.)
- **No spine listing endpoint.** Discover chapters by iterating `?index=0..` until
  `404`, or by following `chapter_href` from `location`. `chapter_href` is a full
  zip path (e.g. `OEBPS/chapter05.xhtml`); the same value works as `?href=`.
- **`chapter_href` may be absent** in `location` if a CFI can't be resolved to a
  spine item — `cfi`/`progress`/`updated_at` are always present.
- **404s** also mean: book id unknown, or the book's EPUB blob is missing on disk.

## What Eidolon needs from the Lyceum operator

1. The reachable base URL (`http://<host>:<LYCEUM_ADDR port>`).
2. An `eidolon:read` token (provisioned via `LYCEUM_API_TOKENS`).
3. Confirmation the box is reachable from where Eidolon runs.

Lyceum repo: `~/projects/lyceum` · API source: `internal/api/eidolon.go` ·
contract: `docs/eidolon-api.md`.
