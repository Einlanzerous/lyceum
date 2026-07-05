# Lyceum desktop (Wails / Windows)

The Windows `.exe` shell for the Lyceum reader (LYCM-300). It hosts the **same**
TypeScript SPA as the web build inside a native WebView2 window. It ships no
backend — it is a thin client that talks to your Lyceum server over HTTP, so on
first launch it asks for your server URL (Settings → Connection).

## How it fits together

- The SPA is built with `npm run build:native` (`web/`), which flips
  `import.meta.env.VITE_LYCEUM_TARGET` to `native`. In that mode
  `web/src/api/base.ts` prefixes every API call with the configured server URL
  instead of using same-origin relative URLs.
- `copy-dist.mjs` copies `web/dist` into `frontend/dist`, which `main.go`
  embeds via `//go:embed all:frontend/dist`.
- Wails serves those assets from `http://wails.localhost`. That origin is in the
  backend's CORS allowlist (`internal/api.DefaultCORSOrigins`), so the
  cross-origin API calls succeed without extra server config.

## Prerequisites (on the build machine)

- Go 1.25+
- The Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Node 20+ (for the frontend build)
- Windows host **or** a Linux host set up for Wails Windows cross-compilation.
  Run `wails doctor` to confirm your toolchain.

## Build

From the repository root:

```sh
make wails-windows          # builds the SPA, copies it in, runs `wails build`
```

or directly in this directory:

```sh
wails build -platform windows/amd64
```

The first build runs `go mod tidy` for this module (resolving Wails) and the
`frontend:build` step in `wails.json` (web build + `copy-dist.mjs`). The output
`.exe` lands in `build/bin/Lyceum.exe`.

### Two flavors: generic vs. "my library"

By default the `.exe` prompts for a server URL on first run — the right build for
other self-hosters, who point it at their own server. To ship a **zero-config**
build for friends & family pointed at *your* server, bake the URL in via
`VITE_LYCEUM_DEFAULT_SERVER` (the build env flows through `copy-dist.mjs` →
`npm run build:native`):

```sh
VITE_LYCEUM_DEFAULT_SERVER=http://your-server:5174 make wails-windows
```

That build skips the first-run prompt and loads the library immediately; the
address is still editable in Settings → Connection. An unset value (the plain
`make wails-windows` above) leaves the prompt in place. See
[`web/src/api/base.ts`](../../web/src/api/base.ts) for the resolution order
(saved URL → baked default → prompt).

Live development against a running backend:

```sh
wails dev
```

## Installer & releases

`make wails-windows` produces the bare `Lyceum.exe`. For distribution, build an
**NSIS installer** instead — it installs to Program Files, creates Start-menu and
desktop shortcuts, registers an uninstaller, and bootstraps the WebView2 runtime:

```sh
wails build -platform windows/amd64 -skipbindings -nsis   # needs makensis (NSIS)
# → build/bin/Lyceum-amd64-installer.exe
```

The installer is defined by [`build/windows/installer/project.nsi`](build/windows/installer/project.nsi)
(tracked, customizable); `wails_tools.nsh` next to it is regenerated on every
build and git-ignored. The version shown in Add/Remove Programs comes from
`wails.json` → `info.productVersion`.

CI does this automatically: pushing a **`v*` tag** runs
[`.github/workflows/release.yml`](../../.github/workflows/release.yml), which
stamps the version from the tag, builds the installer on a Windows runner
(NSIS is preinstalled), and publishes it + the portable `.exe` to a GitHub
Release. A manual `workflow_dispatch` uploads the installer as a run artifact
without publishing.

### Code signing (opt-in)

Unsigned installers trip SmartScreen, which non-technical users won't click
through — sign before sharing beyond yourself. The release workflow signs the
installer automatically **when** the repo has these secrets (skipped otherwise):

- `WINDOWS_PFX_BASE64` — base64 of your code-signing `.pfx`
- `WINDOWS_PFX_PASSWORD` — its password

To also sign the inner `Lyceum.exe`, split the build (build the `.exe` → sign it
→ run `makensis` on `project.nsi`), or wire the commented `!finalize` /
`!uninstfinalize` `signtool` hooks in `project.nsi`.

## Notes

- This is a separate Go module (`github.com/magos/lyceum/wrappers/wails`) so the
  heavy Wails/WebView2 dependency tree never touches the backend module. The
  root `go build ./...` does not descend here.
- End users need the Microsoft **WebView2** runtime, which is preinstalled on
  current Windows 10/11. Use `wails build -webview2 embed` to bundle it if you
  must target machines without it.
