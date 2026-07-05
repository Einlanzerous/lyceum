# LYCM-500 (Reader Settings & Preferences) — state & handoff

Where things stand after the Phase 2 web reader, the design pass, and the first
settings ticket — and exactly how to pick up the rest.

## Done and on `main`

- **Phase 1 (LYCM-100)** — Postgres-backed Go backend: `/upload /library /sync
  /books/{id}/{cover,file}`, EPUB parse + CFI validation, LWW position sync.
- **Phase 2 (LYCM-200)** — Vue 3 + Vite + TS + epub.js reader: typed client,
  library (grid/list + upload), reader (paged render, nav, font-size, position
  capture/restore + unload flush), and same-origin SPA serving from Go
  (`//go:embed`, `web/embed.go`).
- **Design system (Lyceum.dc.html)** — dark default + light "warm paper",
  brass-on-charcoal, self-hosted Archivo + Hanken Grotesk (`@fontsource`). All
  screens + states restyled.
- **LYCM-501** — shared `ThemeToggle`, library-level theme toggle, and a
  `/settings` page (Appearance: Dark/Light; Reading: font slot reserved).

49 vitest unit tests + 3 Playwright smokes (system Chrome) + Go build/vet/test
all green.

## Next up: LYCM-502 — custom reading font (the open child of LYCM-500)

Default stays **publisher fonts**; this adds an *opt-in* override. The settings
surface and the slot are already built — wire in the control + apply path.

Integration points (already in the tree):
- **Settings UI** — `web/src/views/SettingsView.vue`, the "Reading" group. The
  `Custom reading font` row is a `Coming soon` placeholder; replace it with a
  family picker (default "Publisher" = no override, plus a serif — the mock's
  Georgia — a sans, and ideally a dyslexia-friendly face).
- **Apply to epub.js** — `web/src/reader/theme.ts` `themeStyles()` builds the
  `lyceum` theme rule object that `useReader` registers/selects. Add a
  `font-family` rule (or use `rendition.themes.font(family)`), keyed off the
  chosen font; "Publisher" omits the rule. `useReader` (`web/src/reader/use
  Reader.ts`) already re-applies the theme on change via `watch(theme, apply
  Theme)` — mirror that for the font choice.
- **State** — add a persisted singleton like `web/src/theme/index.ts`
  (localStorage, default "publisher"); a `useReadingFont()` returning a reactive
  `font` ref + `set()`. Keep it independent of font *size* (`FONT_SIZES` /
  `stepFontSize`, already in `reader/theme.ts`).
- **Bundled faces** — self-host via `@fontsource` (offline LAN), same as the UI
  fonts; import the chosen weights in `main.ts` / `styles/main.css`.

Done when: choosing a font re-renders the open book live, "Publisher" restores
the book's own fonts, and the choice persists across reloads/books. Unit-test
the pure resolve-family→rule helper; verify rendering via the reader smoke.

## Testing reality (unchanged from Phase 2 — read before trusting green)

epub.js renders into a real iframe; it is **not** meaningfully testable under
jsdom. Keep pure logic in vitest; verify rendering with the Playwright smoke
(`web/e2e/`, `npm run test:e2e`) driving **system Chrome** (`channel: 'chrome'`,
no bundled download). The repo's testdata EPUBs are single-page, so
`web/e2e/make-fixture.mjs <out> <unique-tag>` generates a multi-page book; pass a
fresh tag to avoid the content-addressed 409 and get a position-free book.

## Dev setup (this environment)

```sh
# Port 8080 is taken by Dozzle here, so run the backend elsewhere:
set -a && . ./.env && set +a
LYCEUM_ADDR=:8099 LYCEUM_DATA_DIR=$(mktemp -d) go run ./cmd/lyceum &

# Dev server proxies the API to that backend; --host exposes it on the LAN:
cd web && LYCEUM_BACKEND=http://localhost:8099 npm run dev -- --host --port 5180 --strictPort
# desktop: http://<server-ip>:5180/
```

## Gotchas already caught (don't re-discover these)

- **`crypto.randomUUID` is secure-context only.** Plain-HTTP LAN access (the
  real deployment) lacks it; `getDeviceId()` (`web/src/api/device.ts`) falls back
  to `getRandomValues`. Any future secure-context-only API (`crypto.subtle`,
  service workers) has the same trap.
- **Root `.gitignore` `lyceum` was unanchored** and silently excluded
  `cmd/lyceum/` — now `/lyceum`. Watch for broad bare patterns.
- **`//go:embed` needs `web/dist/`** to exist; a checked-in `web/dist/.gitkeep`
  keeps `go build` working without a web build (`make build-web` / `make release`
  produce the real bundle).
- **epub.js paginates with CSS columns** → `body.innerText` is constant within a
  section; the CFI is the page-turn signal. The reader exposes `data-cfi` /
  `data-progress` on `.reader` for tests. `display(cfi)` normalizes to the
  column-start CFI (restore is page-accurate, not byte-identical).
- Reading surface is a centered single column (`spread: 'none'`); don't let
  epub.js two-up. Don't put the unifying hatch over real cover art (fallback
  tiles only).

## Beyond LYCM-500

Untouched epics: **LYCM-300** (Cross-Platform Wrappers — Wails desktop; the
Android app is the native Flutter project under `mobile/`, LYCM-700) and
**LYCM-400** (Ecosystem & Agent Integration — Send-to-
Kindle, Eidolon TTS hooks per `docs/eidolon-api.md`, auth/token scheme). The
design project also sketches a scroll-mode reader (frame 8) and a TTS widget
(frame 11, EIDO) not yet built. Design source: the Claude Design project
`Lyceum.dc.html`.
