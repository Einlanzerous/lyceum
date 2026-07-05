// Resolves the base URL the API client talks to. This is the seam that lets the
// *same* TypeScript reader run in two shells (LYCM-300):
//
//   - Web (default): the Go server serves this bundle and the JSON API from one
//     origin (LYCM-207), so every API URL is same-origin *relative* ('' base).
//   - Wails (Windows .exe): the SPA is served by Wails from `http://wails.localhost`
//     and the backend lives on a remote home server — a different origin.
//
// In the native shell there is no same-origin backend, so the app must be told
// the server's URL. That resolves from one of two places: a build-time default
// baked in via VITE_LYCEUM_DEFAULT_SERVER (so a "my library" build for friends &
// family ships pre-pointed, zero-config), which whatever the user saves at
// runtime (localStorage) overrides. We prefix every API call with the result.
// The backend grows a CORS allowlist (internal/api.CORS) covering the Wails
// origin so these cross-origin calls succeed.
//
// (The Android app is a separate native Flutter project, not this SPA.)

const SERVER_KEY = 'lyceum.server_url'

// Build-time target, injected by Vite (see vite.config.ts `define`). 'native'
// is set by `npm run build:native`, which the wrappers consume; an ordinary
// `npm run build` leaves it 'web'. We key off a build flag rather than sniffing
// `window.Capacitor`/Wails at runtime because the decision we actually need —
// "is the backend a different origin I must be told about?" — is fixed at build
// time, and a flag is trivially testable under jsdom.
// Written as the bare `import.meta.env.VITE_LYCEUM_TARGET` member expression so
// Vite's `define` (vite.config.ts) replaces it with a string literal at build —
// a cast or optional chaining here would break that syntactic match. The type
// comes from the ImportMetaEnv augmentation in env.d.ts. Undefined under vitest
// (no define) → 'web', the safe default.
const BUILD_TARGET: string = import.meta.env.VITE_LYCEUM_TARGET ?? 'web'

// Build-time default backend URL (VITE_LYCEUM_DEFAULT_SERVER), replaced by Vite's
// `define` (vite.config.ts) exactly like BUILD_TARGET above — hence the bare
// member expression. A "my library" native build bakes the home server here so
// first run is zero-config; the generic self-hoster build and the web build
// leave it '' and prompt instead. Normalized once (trimmed, no trailing slash).
const BUILD_DEFAULT_SERVER: string = normalizeServerUrl(
  import.meta.env.VITE_LYCEUM_DEFAULT_SERVER ?? '',
)

// Test seam: when non-null, overrides the build-target check so unit tests can
// exercise both shells without rebuilding. Production code never sets this.
let nativeOverride: boolean | null = null

// Test seam: overrides the baked default server URL. null = use the build value.
let defaultOverride: string | null = null

/**
 * The baked default backend URL ('' when unset), honoring the test override.
 * Normalized so a test override behaves exactly like the load-normalized
 * BUILD_DEFAULT_SERVER (normalize is idempotent, so re-running it is harmless).
 */
function defaultServer(): string {
  return normalizeServerUrl(defaultOverride ?? BUILD_DEFAULT_SERVER)
}

// undefined = not yet read from storage; null = read, none set.
let serverCache: string | null | undefined

/**
 * True when running as a native shell (Wails) that must be pointed at a remote
 * backend. False for the web build served same-origin by the Go server.
 */
export function isNativeShell(): boolean {
  if (nativeOverride !== null) return nativeOverride
  return BUILD_TARGET === 'native'
}

/** Strip surrounding whitespace and any trailing slashes from a server URL. */
export function normalizeServerUrl(url: string): string {
  return url.trim().replace(/\/+$/, '')
}

/**
 * The backend URL for native shells: the URL the user saved, else the build-time
 * default (BUILD_DEFAULT_SERVER), else '' when neither is set. Cached after first
 * read. Clearing a saved URL therefore reverts to the baked default, not to ''.
 */
export function getServerUrl(): string {
  if (serverCache !== undefined) return serverCache ?? defaultServer()
  try {
    serverCache = localStorage.getItem(SERVER_KEY)
  } catch {
    serverCache = null
  }
  return serverCache ?? defaultServer()
}

/**
 * Persist the backend URL the native shell should call. Passing an empty value
 * clears it. The value is normalized (trimmed, no trailing slash) so callers
 * can pass whatever the user typed.
 */
export function setServerUrl(url: string): void {
  const normalized = normalizeServerUrl(url)
  serverCache = normalized || null
  try {
    if (normalized) localStorage.setItem(SERVER_KEY, normalized)
    else localStorage.removeItem(SERVER_KEY)
  } catch {
    // localStorage blocked (private mode): keep the in-memory value so the
    // current session still works.
  }
}

/**
 * True when the app is ready to make API calls: always so in web mode; in a
 * native shell only once a server URL has been configured. The UI uses this to
 * prompt for a server on first run instead of failing opaque fetches.
 */
export function hasBackend(): boolean {
  return !isNativeShell() || getServerUrl() !== ''
}

/**
 * The origin to prefix API paths with. '' (same-origin relative) for web;
 * the configured server for native, or '' when unconfigured — in which case
 * calls resolve against the shell origin and fail, which hasBackend() gates.
 */
export function apiBase(): string {
  if (!isNativeShell()) return ''
  return getServerUrl()
}

/** Resolve an API path (e.g. '/library') against the active backend base. */
export function apiUrl(path: string): string {
  return apiBase() + path
}

/** Test seam: force/clear native-shell mode. Pass null to restore the build flag. */
export function __setNativeShell(value: boolean | null): void {
  nativeOverride = value
}

/** Test seam: set/clear the baked default server URL. Pass null for the build value. */
export function __setDefaultServer(value: string | null): void {
  defaultOverride = value
}

/** Test seam: drop the cached server URL so the next read hits storage again. */
export function __resetServerCache(): void {
  serverCache = undefined
}
