# Handoff: cross-platform wrappers (LYCM-300)

> **Superseded (Android):** the Capacitor web-shell described below was replaced
> by a native **Flutter** Android app (`mobile/lyceum`, LYCM-700) and has been
> removed from the tree. The Wails desktop shell is the live wrapper; the
> Capacitor sections here are retained only as a record of what was built. See
> [`wrappers/README.md`](../wrappers/README.md) for the current state.

Phase 3 packages the Lyceum web reader as native apps **reusing the same
TypeScript frontend**: a Windows `.exe` (Wails) and a sideloadable Android
`.apk` (Capacitor). Both are thin clients that reach a remote Lyceum server and
sync exactly like the browser reader.

## What changed (and why)

The web build is served by the Go backend on the same origin, so its API calls
are relative. A native shell loads the SPA from its own origin with no
same-origin backend, so two seams were added:

1. **Backend-base-URL-aware frontend.**
   - [`web/src/api/base.ts`](../web/src/api/base.ts) resolves the API base.
     Web build → `''` (relative, unchanged). Native build (`VITE_LYCEUM_TARGET=native`,
     set by `npm run build:native` / `vite build --mode native`) → a server URL
     the user configures, persisted in `localStorage` (`lyceum.server_url`).
   - `client.ts` routes every URL through `apiUrl()`; `coverUrl()`/`bookFileUrl()`
     become absolute in native shells (they back `<img src>` and the epub fetch).
   - [`ServerSettings.vue`](../web/src/components/ServerSettings.vue) is the
     connection editor — shown in Settings → Connection and as the Library's
     first-run prompt (native only). `useServer.ts` shares the choice reactively.

2. **CORS on the backend.** [`internal/api/cors.go`](../internal/api/cors.go)
   wraps the whole mux (in `cmd/lyceum/main.go`). The fixed native origins
   (`DefaultCORSOrigins`: `http://wails.localhost`, `http://localhost`, …) are
   allowed out of the box; `LYCEUM_CORS_ORIGINS` extends them (or `*`). Preflight
   `OPTIONS` is answered here before the method-specific routes can 405 it. The
   same-origin web build sends no `Origin` and is unaffected.

## The wrappers

- [`wrappers/wails/`](../wrappers/wails/) — separate Go module
  (`github.com/magos/lyceum/wrappers/wails`) so the Wails/WebView2 dep tree never
  touches the backend. `main.go` embeds `frontend/dist` (copied from `web/dist`
  by `copy-dist.mjs`).
- [`wrappers/capacitor/`](../wrappers/capacitor/) — Capacitor project; `copy-web.mjs`
  builds + copies the SPA into `www`. `androidScheme: 'http'` + a cleartext
  network-security config let it reach an http home server.

## Building the artifacts

Both artifacts have been **built and verified on this dev box** using the
toolchain at `~/dev-tools` (the one argosy set up): JDK 17, Android SDK
(platforms 34/35/36, build-tools 34), and the Wails CLI installed via
`go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1`. Source the toolchain
env first, then build:

```sh
source ~/dev-tools/env.sh                    # JAVA_HOME, ANDROID_HOME, flutter+sdk on PATH
export PATH="$(go env GOPATH)/bin:$PATH"     # wails CLI

# Android .apk (~4.4 MB) → wrappers/capacitor/android/app/build/outputs/apk/debug/app-debug.apk
cd wrappers/capacitor && npm install && npm run add:android   # one-time: generates android/
make android-apk                              # copy-web → cap sync → gradlew assembleDebug

# Windows .exe (~11 MB) → wrappers/wails/build/bin/Lyceum.exe
make wails-windows                            # cross-compiles from Linux (-skipbindings)
```

Notes:
- The Windows `.exe` **cross-compiles from Linux** because Wails loads WebView2
  at runtime (no CGO). `-skipbindings` is required for the cross-build (binding
  generation runs a Windows probe binary that can't exec on Linux); the app
  binds no Go methods, so this is lossless. The user also has Windows machines
  available if a native `wails build` is ever preferred.
- The Android cleartext tweak is applied reproducibly by
  `apply-android-overrides.mjs` (run from the npm `sync`/`add:android` scripts),
  so a regenerated `android/` always gets it.
- First builds resolve their own deps (`go mod tidy` populated
  `wrappers/wails/go.sum`; `npm install`; Gradle downloads its distribution).

## Verified here

- `web`: 64 unit tests green (incl. `base.test.ts` both shells, native
  LibraryView prompt). Both `npm run build` and `npm run build:native` succeed;
  the `VITE_LYCEUM_TARGET` define is confirmed replaced in both bundles.
- `backend`: `go build/vet ./...` clean; full `go test ./...` green incl.
  `cors_test.go`. End-to-end CORS smoke against the running binary confirmed:
  allowed origins get echoed `Access-Control-Allow-Origin`, preflights return
  204 with the right headers, disallowed origins get none, same-origin untouched.
- **`Lyceum.exe`**: built (11 MB, `PE32+ executable (GUI) x86-64, for MS
  Windows`). The native-mode SPA is embedded (`<title>Lyceum</title>`,
  `frontend/dist/assets/index-*.js`) and the WebView2 loader is linked.
- **`app-debug.apk`**: built (4.4 MB). `aapt` confirms `package
  com.lyceum.reader`, label `Lyceum`, compileSdk 34; the APK contains the web
  bundle (`assets/public/index.html`) and the cleartext
  `res/xml/network_security_config.xml`.

## Not done (needs a device / release flow)

- On-device run-through of reading + sync — the exit-criteria smoke that needs a
  real Windows box / Android device pointed at a running Lyceum server. The
  builds compile and embed correctly; behaviour on hardware is unverified here.
- App icons/splash polish, **signed release** APK (the debug APK is sideloadable
  as-is), and Wails auto-update.
