# Lyceum ↔ Project Eidolon API contract

Endpoints Lyceum exposes for [Project Eidolon](../../project-eidolon) to drive
local TTS streaming. Implemented under Epic **LYCM-400** (tickets LYCM-403/404),
protected by the token scheme in **LYCM-405**.

All endpoints require `Authorization: Bearer <token>` with scope `eidolon:read`.

## GET /eidolon/books/{id}/location  (LYCM-403)

Current reading location for a book.

```json
{
  "cfi": "epubcfi(/6/14[chap05]!/4/2/1:0)",
  "progress": 0.42,
  "chapter_href": "OEBPS/chapter05.xhtml",
  "updated_at": "2026-06-26T19:00:00Z"
}
```

## GET /eidolon/books/{id}/chapter  (LYCM-404)

Plain-text, TTS-ready chapter content.

Query params:
- `href` — spine item href (or `index` — 0-based spine index)
- `from_cfi` *(optional)* — start emitting text at this location (resume-from-here)

Returns `text/plain; charset=utf-8` with HTML stripped and paragraph
boundaries preserved. Large chapters are streamed.

---

> Status: **contract draft.** Endpoints land with LYCM-403/404; this file is the
> source of truth for the shape Eidolon should code against.
