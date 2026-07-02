# Lyceum ↔ Project Eidolon API contract

Endpoints Lyceum exposes for [Project Eidolon](../../project-eidolon) to drive
local TTS streaming. Implemented under Epic **LYCM-400** (tickets LYCM-403/404),
protected by the token scheme in **LYCM-405**.

## Authentication

All `/eidolon/*` endpoints require a bearer token carrying the `eidolon:read`
scope:

```
Authorization: Bearer <token>
```

Tokens are issued via the server's `LYCEUM_API_TOKENS` env (see
[README](../README.md#ecosystem--agent-integration-lycm-400)). A missing or
unknown token returns `401`; a valid token without `eidolon:read` returns `403`.
If no tokens are configured the integration surface is closed (every request
`401`s) — issue a token to enable it.

## GET /eidolon/books/{id}/location  (LYCM-403)

Current reading location for a book — the latest synced position across devices.

```json
{
  "cfi": "epubcfi(/6/14[chap05]!/4/2/1:0)",
  "progress": 0.42,
  "chapter_href": "OEBPS/chapter05.xhtml",
  "updated_at": "2026-06-26T19:00:00Z"
}
```

- `chapter_href` is the spine document the CFI resolves to (its full path within
  the EPUB zip). It is **omitted** when the CFI cannot be resolved to a spine
  item (e.g. an unusual CFI shape); `cfi`/`progress` are always present.
- `404` when the book has no synced position yet.

## GET /eidolon/books/{id}/chapter  (LYCM-404)

Plain-text, TTS-ready chapter content.

Query params (exactly one selector, checked in this precedence):
- `index` — 0-based spine index
- `href` — spine item href (the full zip path from `chapter_href`, or the bare
  filename, e.g. `chapter05.xhtml`)
- `from_cfi` — resolve the chapter from a reading CFI (resume-from-here)

Returns `text/plain; charset=utf-8` with HTML stripped and paragraph boundaries
preserved (paragraphs separated by a blank line). `script`/`style` content is
dropped and inline whitespace collapsed. Large chapters are streamed
paragraph-by-paragraph. The resolved document path is echoed in the
`X-Chapter-Href` response header.

Responses:
- `400` — no selector given, or an unparseable `from_cfi`.
- `404` — spine index out of range, href not in the spine, or the book's EPUB
  blob is missing on disk.

> **`from_cfi` resume granularity.** `from_cfi` currently selects the *chapter*
> the CFI lives in and streams it from the start. Sub-chapter (paragraph/offset)
> resume is a planned refinement; for now Eidolon should pair `from_cfi` with its
> own offset bookkeeping if it needs to resume mid-chapter.

---

> Status: **implemented** (LYCM-403/404). This file is the source of truth for
> the shape Eidolon codes against.
