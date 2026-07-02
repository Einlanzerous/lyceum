# Native wrappers (LYCM-300)

Phase 3 packages the Lyceum web reader as native apps **without forking the
frontend**: both shells embed the exact same TypeScript SPA from `web/` and
reach a remote Lyceum server over HTTP.

| Shell                       | Output                | Toolchain            |
|-----------------------------|-----------------------|----------------------|
| [`wails/`](wails/)          | Windows `Lyceum.exe`  | Go + Wails CLI       |
| [`capacitor/`](capacitor/)  | Android `app-debug.apk` | Node + Android SDK |

## The shared mechanism

The web build is served by the Go backend on the same origin, so its API calls
are relative (`/library`, `/sync`, …). The native shells load the SPA from their
own origin (`http://wails.localhost`, `http://localhost`) with no same-origin
backend, so two things change:

1. **Build mode.** `npm run build:native` sets `VITE_LYCEUM_TARGET=native`. In
   that mode [`web/src/api/base.ts`](../web/src/api/base.ts) prefixes every API
   call with a server URL the user configures on first run (Settings →
   Connection, surfaced by `ServerSettings.vue`). The same source builds the web
   app unchanged when the flag is absent.

2. **CORS.** Cross-origin calls from the shells are allowed by the backend's
   CORS middleware ([`internal/api/cors.go`](../internal/api/cors.go)). The fixed
   native origins are allowed by default — no server config needed — and
   `LYCEUM_CORS_ORIGINS` extends the list.

So the data flow is identical to the web reader: list the library, open a book,
and sync reading position via the same REST API.

## Build

Each wrapper rebuilds the SPA in native mode as part of its own build, so you
never ship a stale or web-mode bundle:

```sh
make wails-windows     # → wrappers/wails/build/bin/Lyceum.exe
make android-apk       # → wrappers/capacitor/android/.../app-debug.apk
```

See each subdirectory's README for prerequisites and the one-time setup
(`wails doctor`, `cap add android`, Android cleartext config).
