// Stable per-device identifier for /sync. Generated once and persisted, so a
// device keeps the same id across reloads; the backend keys one reading
// position row per (book_id, device_id).

const STORAGE_KEY = 'lyceum.device_id'

let cached: string | null = null

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
    const id = crypto.randomUUID()
    localStorage.setItem(STORAGE_KEY, id)
    cached = id
    return id
  } catch {
    // localStorage blocked: keep a process-lifetime id rather than throwing.
    cached ??= crypto.randomUUID()
    return cached
  }
}

/** Test seam: drop the in-memory cache so getDeviceId re-reads storage. */
export function resetDeviceIdCache(): void {
  cached = null
}
