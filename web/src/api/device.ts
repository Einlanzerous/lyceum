// Stable per-device identifier for /sync. Generated once and persisted, so a
// device keeps the same id across reloads; the backend keys one reading
// position row per (book_id, device_id).

const STORAGE_KEY = 'lyceum.device_id'

let cached: string | null = null

/**
 * Generates a RFC 4122 v4 UUID, resilient to insecure contexts.
 *
 * crypto.randomUUID() is only defined in *secure* contexts (HTTPS or
 * localhost). Lyceum is meant to run on a home server reached over plain HTTP
 * by LAN IP — an insecure context where randomUUID is undefined — so we fall
 * back to building a v4 UUID from crypto.getRandomValues (which IS available in
 * insecure contexts), and to Math.random only if even that is missing.
 */
function generateUUID(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }

  const bytes = new Uint8Array(16)
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    crypto.getRandomValues(bytes)
  } else {
    for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256)
  }
  // Pin the version (4) and variant bits per RFC 4122.
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80

  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0'))
  return (
    hex.slice(0, 4).join('') +
    '-' +
    hex.slice(4, 6).join('') +
    '-' +
    hex.slice(6, 8).join('') +
    '-' +
    hex.slice(8, 10).join('') +
    '-' +
    hex.slice(10, 16).join('')
  )
}

/**
 * Returns this device's stable id, generating and persisting one on first use.
 * Falls back to an in-memory id if localStorage is unavailable (e.g. private
 * mode), so callers always get a usable, non-empty value.
 */
export function getDeviceId(): string {
  if (cached) return cached

  try {
    const existing = localStorage.getItem(STORAGE_KEY)
    if (existing) {
      cached = existing
      return existing
    }
    const id = generateUUID()
    localStorage.setItem(STORAGE_KEY, id)
    cached = id
    return id
  } catch {
    // localStorage blocked: keep a process-lifetime id rather than throwing.
    cached ??= generateUUID()
    return cached
  }
}

/** Test seam: drop the in-memory cache so getDeviceId re-reads storage. */
export function resetDeviceIdCache(): void {
  cached = null
}

/**
 * A human-readable name for this device, guessed from the user agent (LYCM-801).
 *
 * The sign-in screen shows this pre-filled with a "change" affordance rather than
 * asking for it. Making the front door a two-field form to populate a list most
 * people will look at once is a bad trade; a guess they can correct keeps the
 * door single-field *and* keeps the devices list meaningful — "Chrome on Windows"
 * is enough to recognise the laptop you left at work, which is the only reason
 * that list exists.
 *
 * This is a label, not an identity: getDeviceId above remains what /sync keys on.
 */
export function inferDeviceLabel(): string {
  const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent

  const os = /iPhone/.test(ua)
    ? 'iPhone'
    : /iPad/.test(ua)
      ? 'iPad'
      : /Android/.test(ua)
        ? 'Android'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Mac OS X|Macintosh/.test(ua)
            ? 'Mac'
            : /CrOS/.test(ua)
              ? 'Chromebook'
              : /Linux/.test(ua)
                ? 'Linux'
                : ''

  // Order matters: Edge and Opera both also claim "Chrome", and Chrome also
  // claims "Safari".
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : ''

  if (browser && os) return `${browser} on ${os}`
  return browser || os || 'This device'
}
