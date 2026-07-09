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
  finishedCount: number
}

export type ShelfItem =
  { kind: 'book'; key: string; book: Book } | { kind: 'series'; key: string; series: SeriesGroup }

export function memberStatus(b: Book): MemberStatus {
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
 * The id of the book to pin to the top of the shelf: the most-recently-read book
 * that is still in progress (your "continue reading"). Returns null when nothing
 * is mid-read.
 */
export function pinnedBookId(books: readonly Book[]): number | null {
  let best: Book | null = null
  for (const b of books) {
    if (!b.read_at) continue
    const p = b.progress ?? 0
    if (p <= 0 || p >= FINISHED_AT) continue // only books mid-read
    if (!best || b.read_at > (best.read_at ?? '')) best = b
  }
  return best ? best.id : null
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
  const ordered = [...members].sort((a, b) => {
    const ai = a.series_index ?? Number.POSITIVE_INFINITY
    const bi = b.series_index ?? Number.POSITIVE_INFINITY
    if (ai !== bi) return ai - bi
    return byTitle(a, b)
  })
  const progress = ordered.reduce((sum, m) => sum + (m.progress ?? 0), 0) / ordered.length
  const coverBook = ordered.find((m) => m.cover_url) ?? ordered[0]!
  const finishedCount = ordered.filter((m) => memberStatus(m) === 'finished').length
  return {
    name,
    author: pickAuthor(ordered),
    members: ordered,
    progress,
    coverBook,
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

  const items: ShelfItem[] = []
  for (const b of loose) {
    items.push({ kind: 'book', key: `book-${b.id}`, book: b })
  }
  for (const [key, g] of groups) {
    if (g.members.length === 1) {
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
