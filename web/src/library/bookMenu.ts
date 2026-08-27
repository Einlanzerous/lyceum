import type { Book } from '@/api/types'

/** One entry in a book's context menu. `danger` styles destructive actions. */
export interface BookMenuItem {
  key: 'edit' | 'finish' | 'remove'
  label: string
  danger?: boolean
}

/**
 * The per-book actions offered wherever a book is drawn — the grid card, the
 * series drawer, and the list row. Shared so the three cannot drift: they did,
 * once. The drawer derived its label from `memberStatus()`, which calls a book
 * at 99% "finished" even when it is not marked read, so its toggle claimed
 * "Mark as unread" and then wrote back the value the book already had.
 *
 * Read/unread is therefore keyed off the explicit `finished` flag, never the
 * progress heuristic: the menu writes that flag, so it has to label it.
 */
export function bookMenuItems(book: Book): BookMenuItem[] {
  return [
    // Title/author/series are otherwise fixed at ingest — an EPUB with no
    // series metadata stays series-less for good (LYCM-129).
    { key: 'edit', label: 'Edit details…' },
    { key: 'finish', label: isFinished(book) ? 'Mark as unread' : 'Mark as read' },
    // Destructive and unreachable elsewhere on the shelf (LYCM-109); every
    // caller confirms before acting on it.
    { key: 'remove', label: 'Remove from library', danger: true },
  ]
}

/** Whether the book is explicitly marked read — what the menu toggles. */
export function isFinished(book: Book): boolean {
  return book.finished === true
}
