// The editable subset of a book, as form text, and the patch it becomes
// (LYCM-129). Shared by the review queue's inline form and the library's edit
// dialog so the two cannot disagree about how a series number is read.
import type { Book } from '@/api/types'
import type { BookPatch } from '@/api/client'

export interface BookDraft {
  title: string
  author: string
  series: string
  /** As typed — "3", "3.5", or "" for none. Parsed on save. */
  seriesIndex: string
}

export function draftOf(book: Book): BookDraft {
  return {
    title: book.title,
    author: book.author,
    series: book.series ?? '',
    seriesIndex: book.series_index != null ? String(book.series_index) : '',
  }
}

/**
 * The PATCH body for a draft. Series is always sent (so clearing the field
 * clears the series); the index is sent as 0 when blank or unparseable, which
 * the server stores as "no position".
 */
export function patchOf(d: BookDraft): BookPatch {
  const series = d.series.trim()
  const parsed = Number.parseFloat(d.seriesIndex.trim())
  const index = series && Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
  return { title: d.title.trim(), author: d.author.trim(), series, series_index: index }
}

/** Copy a saved book's editable fields back onto the local one. */
export function applySaved(target: Book, saved: Book): void {
  target.title = saved.title
  target.author = saved.author
  target.series = saved.series
  target.series_index = saved.series_index
}
