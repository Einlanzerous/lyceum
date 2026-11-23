# Lyceum

Go server (`cmd/lyceum`, `internal/`), Vue SPA (`web/`), Flutter app
(`mobile/lyceum/`), Wails/Capacitor wrappers (`wrappers/`). What a PR review is for,
and how the repo works in detail, is in `REVIEW.md`.

## Browser target

Firefox is the primary browser. Floor: Firefox 160 (the Firefox release when this was set; the quarterly `frontend-audit` run raises it). Anything newer is progressive enhancement behind `@supports` or feature detection.

`web/vite.config.ts` carries the matching `build.target` (`es2022`, `firefox160`, plus the
Chrome and Safari versions Baseline Widely available implies). Raise both together.
