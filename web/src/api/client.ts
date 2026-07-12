// Typed client over the Phase 1 Go backend. Every URL is resolved through
// apiUrl() (./base): in the web build that yields same-origin relative URLs —
// the Vite proxy forwards them in dev (LYCM-201) and the Go server serves them
// in prod (LYCM-207); in the native shells (Wails/Capacitor, LYCM-300) it
// prefixes the user-configured remote backend.

import type { Book, Position, PositionInput } from './types'
import { apiUrl } from './base'

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

export async function readError(res: Response): Promise<ApiError> {
  let body = ''
  try {
    body = (await res.text()).trim()
  } catch {
    // ignore: fall back to the status-derived message
  }
  return new ApiError(res.status, body)
}

/** URL of a book's cover image (absolute in native shells, relative on web). */
export function coverUrl(id: number): string {
  return apiUrl(`/books/${id}/cover`)
}

/** URL of a book's EPUB file (Range-supported, served by the backend). */
export function bookFileUrl(id: number): string {
  return apiUrl(`/books/${id}/file`)
}

/** GET /library — every book in the collection. */
export async function listLibrary(): Promise<Book[]> {
  const res = await fetch(apiUrl('/library'))
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book[]
}

/** GET /books/{id} — a single book's wire shape (cover, progress, finished). */
export async function getBook(id: number): Promise<Book> {
  const res = await fetch(apiUrl(`/books/${id}`))
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book
}

/** PUT /books/{id}/finished — mark a book read (true) or unread (false). */
export async function setBookFinished(id: number, finished: boolean): Promise<void> {
  const res = await fetch(apiUrl(`/books/${id}/finished`), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ finished }),
  })
  if (!res.ok) throw await readError(res)
}

/**
 * POST /upload — ingest an EPUB. Resolves to the created book (201). A duplicate
 * (409) surfaces as an ApiError so the caller can message it distinctly.
 */
export async function uploadBook(file: File): Promise<Book> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(apiUrl('/upload'), { method: 'POST', body: form })
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
  const res = await fetch(apiUrl(`/sync?${params.toString()}`))
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
  const res = await fetch(apiUrl('/sync'), {
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
  void fetch(apiUrl('/sync'), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    keepalive: true,
  })
}

// --- Ingest QC review queue (LYCM-58) ---

/** GET /ingest/review — books held for review, each with its detected flags. */
export async function listPendingReview(): Promise<Book[]> {
  const res = await fetch(apiUrl('/ingest/review'))
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book[]
}

/** POST /books/{id}/approve — publish a pending book to the shelf. */
export async function approveBook(id: number): Promise<Book> {
  const res = await fetch(apiUrl(`/books/${id}/approve`), { method: 'POST' })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book
}

/** PATCH /books/{id} — correct a book's title/author. Title is required. */
export async function updateBook(id: number, title: string, author: string): Promise<Book> {
  const res = await fetch(apiUrl(`/books/${id}`), {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, author }),
  })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book
}

/** POST /books/{id}/cover — replace a cover from an uploaded image file. */
export async function replaceCover(id: number, file: File): Promise<Book> {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(apiUrl(`/books/${id}/cover`), { method: 'POST', body: form })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book
}

/** POST /books/{id}/cover/refetch — re-fetch a cover from the art source. */
export async function refetchCover(id: number): Promise<Book> {
  const res = await fetch(apiUrl(`/books/${id}/cover/refetch`), { method: 'POST' })
  if (!res.ok) throw await readError(res)
  return (await res.json()) as Book
}

/** DELETE /books/{id} — remove a book (used to reject a pending ingest). */
export async function deleteBook(id: number): Promise<void> {
  const res = await fetch(apiUrl(`/books/${id}`), { method: 'DELETE' })
  if (!res.ok) throw await readError(res)
}
