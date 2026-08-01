// Series roll-up (LYCM-36). Books that share a series collapse into one card;
// grouping happens at render time from the flat library — there is no separate
// series entity. See Series-Feature-Handoff for the visual contract.
import type { Book } from '@/api/types'
import { byTitle, compareFields, type SortState } from './sort'

/** A book is "finished" once progress reaches (near) 1. */
export const FINISHED_AT = 0.99

export type MemberStatus = 'finished' | 'in-progress' | 'not-started'

export interface SeriesGroup {
  /** Display name (the first-seen casing of the series). */
  name: string
  /** Series author — the most common author among members, else the first. */
  author: string
  /** Members in reading order (by series_index, then title). */
  members: Book[]
  /** Aggregate progress 0..1 — the mean of member progress. */
  progress: number
  /** The book whose cover represents the stack (first with a cover, else first). */
  coverBook: Book
  /**
   * The volume you are up to — resumeIndex applied to members. The card wears
   * its cover, the drawer resumes into it, and pinnedBookId hands back its id
   * when the volume you last read is finished (LYCM-108).
   *
   * coverBook is usually this book but not always: a resume volume with no cover
   * art falls back to another member's, so the stack still shows a real cover.
   */
  resumeBook: Book
  finishedCount: number
}

export type ShelfItem =
  { kind: 'book'; key: string; book: Book } | { kind: 'series'; key: string; series: SeriesGroup }

export function memberStatus(b: Book): MemberStatus {
  if (b.finished) return 'finished' // explicit mark-as-read wins over the % heuristic
  const p = b.progress ?? 0
  if (p >= FINISHED_AT) return 'finished'
  if (p > 0) return 'in-progress'
  return 'not-started'
}

/**
 * The book "Resume" should open: the furthest in-progress volume (members are in
 * reading order, so the last in-progress one is furthest along), else the first
 * unstarted volume, else — everything read — the first volume.
 */
export function resumeIndex(members: Book[]): number {
  let lastInProgress = -1
  let firstUnstarted = -1
  members.forEach((b, i) => {
    const s = memberStatus(b)
    if (s === 'in-progress') lastInProgress = i
    else if (s === 'not-started' && firstUnstarted === -1) firstUnstarted = i
  })
  if (lastInProgress !== -1) return lastInProgress
  if (firstUnstarted !== -1) return firstUnstarted
  return 0
}

function normalizeKey(series: string): string {
  return series.trim().toLowerCase()
}

/**
 * A series needs this many volumes to roll up into one card; below it its books
 * stay loose. buildShelf and pinnedBookId have to agree on the threshold, so
 * they share it.
 */
const SERIES_MIN_MEMBERS = 2

/** Split books into series groups (keyed by normalized name) and loose books. */
function groupBySeries(books: readonly Book[]): {
  groups: Map<string, { name: string; members: Book[] }>
  loose: Book[]
} {
  const groups = new Map<string, { name: string; members: Book[] }>()
  const loose: Book[] = []
  for (const b of books) {
    const series = (b.series ?? '').trim()
    if (!series) {
      loose.push(b)
      continue
    }
    const key = normalizeKey(series)
    const g = groups.get(key)
    if (g) g.members.push(b)
    else groups.set(key, { name: series, members: [b] })
  }
  return { groups, loose }
}

/** Members in reading order: by series_index, then title. */
function orderMembers(members: readonly Book[]): Book[] {
  return [...members].sort((a, b) => {
    const ai = a.series_index ?? Number.POSITIVE_INFINITY
    const bi = b.series_index ?? Number.POSITIVE_INFINITY
    if (ai !== bi) return ai - bi
    return byTitle(a, b)
  })
}

/**
 * The books making up the shelf item that holds `book` — its series' members
 * when that series rolls up into a card, else the book on its own.
 */
function itemMembers(book: Book, groups: Map<string, { members: Book[] }>): Book[] {
  const g = groups.get(normalizeKey(book.series ?? ''))
  return g && g.members.length >= SERIES_MIN_MEMBERS ? g.members : [book]
}

/**
 * The book to continue — pinned to the top of the shelf, and what the grid
 * card's "Continue" chip opens. Every view takes its pin from this one id, so
 * the grid, the list and the chip cannot disagree about where you left off.
 *
 * It is *not* simply the last book you touched. The pin follows the most
 * recently read book whose **shelf item** still has something left to read
 * (LYCM-108): finish volume 2 of a trilogy and the series is still what you are
 * reading, so its card stays pinned — and the id handed back is volume 3, not
 * the volume you just closed.
 *
 * Two things are deliberately excluded:
 *
 * - **Dead ends.** A finished standalone or a fully-read series is skipped, and
 *   the search falls through to the next most recent, so finishing a one-off
 *   leaves whatever else you are part-way through at the top instead of
 *   clearing the slot.
 * - **Books you only opened.** read_at is stamped from any saved position,
 *   including the progress=0 one a still-open reader flushes before pagination
 *   settles (see GetFurthestPosition), so a book at 'not-started' has not
 *   actually been read and must not take the pin from your real current read.
 *
 * Returns null when nothing you have read leads anywhere. It keys off the newest
 * read_at, so reading anything else moves the pin on its own.
 */
export function pinnedBookId(books: readonly Book[]): number | null {
  const { groups } = groupBySeries(books)

  let best: Book | null = null
  let bestItem: Book[] = []
  for (const b of books) {
    if (!b.read_at) continue
    if (memberStatus(b) === 'not-started') continue
    const members = itemMembers(b, groups)
    if (!members.some((m) => memberStatus(m) !== 'finished')) continue // dead end
    if (!best || b.read_at > (best.read_at ?? '')) {
      best = b
      bestItem = members
    }
  }
  if (!best) return null

  // Still part-way through it: that is exactly where you continue. Otherwise it
  // is a finished volume of a series that (checked above) has one left, so hand
  // back that volume rather than the one just closed.
  if (memberStatus(best) !== 'finished') return best.id
  const ordered = orderMembers(bestItem)
  return ordered[resumeIndex(ordered)]!.id
}

function pickAuthor(members: Book[]): string {
  const counts = new Map<string, number>()
  for (const m of members) {
    const a = m.author.trim()
    if (a) counts.set(a, (counts.get(a) ?? 0) + 1)
  }
  let best = ''
  let bestN = 0
  for (const [a, n] of counts) {
    if (n > bestN) {
      best = a
      bestN = n
    }
  }
  return best || members[0]?.author || ''
}

function buildGroup(name: string, members: Book[]): SeriesGroup {
  const ordered = orderMembers(members)
  // A marked-read volume counts as fully done in the aggregate even if its
  // scroll position never reached 100%.
  const progress =
    ordered.reduce((sum, m) => sum + (m.finished ? 1 : (m.progress ?? 0)), 0) / ordered.length
  // The card wears the cover of the volume you're on (the resume target —
  // defaults to book 1 until you progress), so it reflects where you are in the
  // series. Fall back to the first member with any cover, then to book 1.
  const onBook = ordered[resumeIndex(ordered)]!
  const coverBook = onBook.cover_url ? onBook : (ordered.find((m) => m.cover_url) ?? ordered[0]!)
  const finishedCount = ordered.filter((m) => memberStatus(m) === 'finished').length
  return {
    name,
    author: pickAuthor(ordered),
    members: ordered,
    progress,
    coverBook,
    resumeBook: onBook,
    finishedCount,
  }
}

/** The most recent added_at among a set of books (for the "recently added" sort). */
function newestAdded(books: readonly Book[]): string {
  return books.reduce((max, b) => {
    const a = b.added_at ?? ''
    return a > max ? a : max
  }, '')
}

/**
 * Group books into shelf items and order them by `sort`. A series of ≥2 becomes
 * one series card; a series of 1 (or no series) stays a normal book card, so the
 * grid mixes loose books and series freely. Grouping preserves first-seen order
 * of series so the result is deterministic before sorting.
 */
export function buildShelf(
  books: readonly Book[],
  sort: SortState,
  pinBookId?: number | null,
): ShelfItem[] {
  const { groups, loose } = groupBySeries(books)

  const items: ShelfItem[] = []
  for (const b of loose) {
    items.push({ kind: 'book', key: `book-${b.id}`, book: b })
  }
  for (const [key, g] of groups) {
    if (g.members.length < SERIES_MIN_MEMBERS) {
      const only = g.members[0]!
      items.push({ kind: 'book', key: `book-${only.id}`, book: only })
    } else {
      items.push({ kind: 'series', key: `series-${key}`, series: buildGroup(g.name, g.members) })
    }
  }

  const withFields = items.map((item) => ({
    item,
    fields:
      item.kind === 'book'
        ? {
            title: item.book.title,
            author: item.book.author,
            added: item.book.added_at ?? '',
            id: item.book.id,
          }
        : {
            title: item.series.name,
            author: item.series.author,
            added: newestAdded(item.series.members),
            // Sort ties between series and loose books break by the lowest member id.
            id: Math.min(...item.series.members.map((m) => m.id)),
          },
  }))

  withFields.sort((a, b) => compareFields(sort.key, a.fields, b.fields))
  if (sort.dir === 'desc') withFields.reverse()
  const ordered = withFields.map((w) => w.item)

  // Pin the shelf item holding the current read to the front — the book if it's
  // loose, or its series card if it belongs to one (keeping the group intact).
  if (pinBookId != null) {
    const at = ordered.findIndex((item) =>
      item.kind === 'book'
        ? item.book.id === pinBookId
        : item.series.members.some((m) => m.id === pinBookId),
    )
    if (at > 0) {
      const [pinned] = ordered.splice(at, 1)
      ordered.unshift(pinned!)
    }
  }
  return ordered
}
