// Typed client over the Phase 1 Go backend. All URLs are same-origin relative:
// in dev the Vite proxy (LYCM-201) forwards them to :8080; in prod the Go
// server serves this bundle and the API from the same origin (LYCM-207).

import type { Book, Position, PositionInput } from './types'

/**
 * Error thrown for any non-2xx response (except GET /sync 404, which maps to
 * null). The backend emits plain-text error bodies via Go's http.Error, so the
 * message is the response text, trimmed.
 */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message || `request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
  }
}

async function readError(res: Response): Promise<ApiError> {
  let body = ''
  try {
    body = (await res.text()).trim()
  } catch {
    // ignore: fall back to the status-derived message
  }
  return new ApiError(res.status, body)
}

/** Relative URL of a book's cover image. */
export function coverUrl(id: number): string {
  return `/books/${id}/cover`
}

/** Relative URL of a book's EPUB file (Range-supported, served by the backend). */
export function bookFileUrl(id: number): string {
  return `/books/${id}/file`
}

/** GET /library — every book in the collection. */
export async function listLibrary(): Promise<Book[]> {
  const res = await fetch('/library')
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book[]
}

/**
 * POST /upload — ingest an EPUB. Resolves to the created book (201). A duplicate
 * (409) surfaces as an ApiError so the caller can message it distinctly.
 */
export async function uploadBook(file: File): Promise<Book> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch('/upload', { method: 'POST', body: form })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book
}

/**
 * GET /sync — the position to resume from for a book on this device. Returns
 * null when the backend has no position for it (404): a fresh book, read from
 * the start. Any other non-2xx throws.
 */
export async function getPosition(bookId: number, deviceId: string): Promise<Position | null> {
  const params = new URLSearchParams({ book_id: String(bookId), device_id: deviceId })
  const res = await fetch(`/sync?${params.toString()}`)
  if (res.status === 404) return null
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Position
}

/**
 * PUT /sync — save a reading position. updated_at is set to now when the caller
 * omits it, matching the backend's own default and letting the client win LWW.
 */
export async function putPosition(pos: PositionInput): Promise<Position> {
  const body: Position = { ...pos, updated_at: pos.updated_at ?? new Date().toISOString() }
  const res = await fetch('/sync', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Position
}

/**
 * Fire-and-forget position save for page unload/backgrounding. navigator.send-
 * Beacon is POST-only but /sync is PUT, so this uses fetch with keepalive:true,
 * which lets the request outlive the page. No response is awaited.
 */
export function putPositionKeepalive(pos: PositionInput): void {
  const body: Position = { ...pos, updated_at: pos.updated_at ?? new Date().toISOString() }
  void fetch('/sync', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    keepalive: true,
  })
}
