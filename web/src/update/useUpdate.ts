// Lightweight update check for the native (Wails) shell (LYCM-300). The web
// build is served by the backend and updates with it, so this only runs in a
// native shell. It asks GitHub for the latest release and, when that's newer
// than the running app, surfaces a dismissible banner linking to the download —
// a notify-and-open nudge, not a silent background updater (that wants signed
// packages first). No Go-bound methods, so the wrapper's -skipbindings holds.
import { ref } from 'vue'
import { isNativeShell } from '@/api/base'

// The app's own version, baked at build time (VITE_APP_VERSION, set from the
// release version in CI). Empty/unparseable for dev/unversioned builds, which
// suppresses the check — a dev build shouldn't nag about "updates".
const CURRENT_VERSION: string = import.meta.env.VITE_APP_VERSION ?? ''

const REPO = 'Einlanzerous/lyceum'
const LATEST_RELEASE_API = `https://api.github.com/repos/${REPO}/releases/latest`
const RELEASES_PAGE = `https://github.com/${REPO}/releases/latest`
const DISMISS_KEY = 'lyceum.update_dismissed' // stores the dismissed version (no leading v)

export type UpdateInfo = { version: string; url: string }

// Test seam: override the baked current version. null = use the build value.
let currentOverride: string | null = null
function currentVersion(): string {
  return currentOverride ?? CURRENT_VERSION
}

/** Parse "v1.2.3" / "1.2.3-rc1" → [1,2,3], ignoring a leading v, pre-release, build. */
function parseVersion(v: string): [number, number, number] | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(v.trim())
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null
}

/** Drop a single leading "v" so stored/compared versions are consistent. */
function stripV(v: string): string {
  return v.trim().replace(/^v/, '')
}

/** True when `candidate` is a strictly newer release than `current`. */
export function isNewerVersion(candidate: string, current: string): boolean {
  const c = parseVersion(candidate)
  const cur = parseVersion(current)
  if (!c || !cur) return false
  for (let i = 0; i < 3; i++) {
    if (c[i] !== cur[i]) return c[i] > cur[i]
  }
  return false
}

// Shared reactive state: the available update, or null.
const update = ref<UpdateInfo | null>(null)

function dismissedVersion(): string {
  try {
    return localStorage.getItem(DISMISS_KEY) ?? ''
  } catch {
    return ''
  }
}

/**
 * Check GitHub for a newer release. No-op unless running as a native shell with
 * a real baked version. Sets the shared `update` ref when a newer, non-dismissed
 * release exists. Best-effort: network/parse/CORS/rate-limit failures are
 * swallowed (no banner, no surfaced error). `fetchImpl` is injectable for tests.
 */
export async function checkForUpdate(fetchImpl: typeof fetch = fetch): Promise<void> {
  if (!isNativeShell()) return
  if (!parseVersion(currentVersion())) return // dev / unversioned build
  try {
    const res = await fetchImpl(LATEST_RELEASE_API, {
      headers: { Accept: 'application/vnd.github+json' },
    })
    if (!res.ok) return
    const body = (await res.json()) as { tag_name?: string; html_url?: string }
    const tag = body.tag_name ?? ''
    if (!isNewerVersion(tag, currentVersion())) return
    const version = stripV(tag)
    if (dismissedVersion() === version) return
    update.value = { version, url: body.html_url || RELEASES_PAGE }
  } catch {
    // Offline / rate-limited / CORS: leave the banner hidden.
  }
}

/** Open a URL in the user's real browser (Wails runtime), falling back to a tab. */
function openExternal(url: string): void {
  const w = window as unknown as { runtime?: { BrowserOpenURL?: (u: string) => void } }
  if (w.runtime?.BrowserOpenURL) w.runtime.BrowserOpenURL(url)
  else window.open(url, '_blank', 'noopener')
}

export function useUpdate() {
  return {
    /** The available update, or null. Reactive, shared across components. */
    update,
    /** The running app's version ('' for dev/unversioned builds). */
    currentVersion: currentVersion(),
    /** Re-run the check. */
    check: () => checkForUpdate(),
    /** Hide the banner and remember this version so it isn't shown again. */
    dismiss(): void {
      if (update.value) {
        try {
          localStorage.setItem(DISMISS_KEY, update.value.version)
        } catch {
          // localStorage blocked: at least hide it for this session.
        }
      }
      update.value = null
    },
    /** Open the release page (or asset) in the browser. */
    openDownload(): void {
      openExternal(update.value?.url ?? RELEASES_PAGE)
    },
  }
}

/** Test seam: set/clear the baked current version. Pass null for the build value. */
export function __setCurrentVersion(v: string | null): void {
  currentOverride = v
}

/** Test seam: clear the shared update state. */
export function __resetUpdate(): void {
  update.value = null
}
