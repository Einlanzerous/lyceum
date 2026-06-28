// Wire shapes mirroring internal/api (Phase 1). Errors from the backend are
// plain text, not JSON — see ApiError in ./client.

/** A single library entry as returned by GET /library and POST /upload. */
export interface Book {
  id: number
  title: string
  author: string
  /** Relative URL to the cover image, or "" when the book has no cover. */
  cover_url: string
  /** Reading progress 0..1; omitted when the book has never been opened. */
  progress?: number
}

/** A device's reading position within a book (GET/PUT /sync). */
export interface Position {
  book_id: number
  device_id: string
  /** EPUB CFI; the backend validates it structurally. */
  cfi: string
  progress: number
  /** ISO-8601 timestamp; drives last-write-wins conflict resolution. */
  updated_at: string
}

/** The fields a client supplies when saving a position (PUT /sync). */
export type PositionInput = Omit<Position, 'updated_at'> & { updated_at?: string }
