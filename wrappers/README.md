# Native desktop wrapper (LYCM-300)

Phase 3 packages the Lyceum web reader as a native **desktop** app **without
forking the frontend**: the shell embeds the exact same TypeScript SPA from
`web/` and reaches a remote Lyceum server over HTTP.

| Shell                       | Output                | Toolchain            |
|-----------------------------|-----------------------|----------------------|
| [`wails/`](wails/)          | Windows `Lyceum.exe`  | Go + Wails CLI       |

> **Android** is a separate, native **Flutter** app under
> [`mobile/`](../mobile/lyceum) (LYCM-700) — not a web-shell wrapper. An earlier
> Capacitor web-shell lived here but was superseded by that app and removed.

## The shared mechanism

The web build is served by the Go backend on the same origin, so its API calls
are relative (`/library`, `/sync`, …). The Wails shell loads the SPA from its
own origin (`http://wails.localhost`) with no same-origin backend, so two things
change:

1. **Build mode.** `npm run build:native` sets `VITE_LYCEUM_TARGET=native`. In
   that mode [`web/src/api/base.ts`](../web/src/api/base.ts) prefixes every API
   call with a server URL the user configures on first run (Settings →
   Connection, surfaced by `ServerSettings.vue`). The same source builds the web
   app unchanged when the flag is absent.

2. **CORS.** Cross-origin calls from the shell are allowed by the backend's
   CORS middleware ([`internal/api/cors.go`](../internal/api/cors.go)). The fixed
   Wails origin is allowed by default — no server config needed — and
   `LYCEUM_CORS_ORIGINS` extends the list.

So the data flow is identical to the web reader: list the library, open a book,
and sync reading position via the same REST API.

## Build

The wrapper rebuilds the SPA in native mode as part of its own build, so you
never ship a stale or web-mode bundle:

```sh
make wails-windows     # → wrappers/wails/build/bin/Lyceum.exe
```

See [`wails/README.md`](wails/README.md) for prerequisites and setup
(`wails doctor`, the Windows cross-compile note).
