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
  /** RFC3339 timestamp the book was ingested; backs the "recently added" sort. */
  added_at?: string
  /** RFC3339 timestamp of the latest reading position; pins the current read. */
  read_at?: string
  /** Series the book belongs to, or "" / omitted when it is a standalone. */
  series?: string
  /** 1-based position within {@link series}; omitted when unknown. */
  series_index?: number
  /** True when the book has been explicitly marked read (independent of progress). */
  finished?: boolean
  /** Ingest-QC status (LYCM-58): "pending" for a held book, omitted when on the shelf. */
  review_state?: string
  /** Detected issue codes for a pending book, e.g. ["no_isbn","suspicious_title"]. */
  review_flags?: string[]
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
