// Which wanted inventory entry a held book most likely fulfils (LYCM-128).
//
// An EPUB that arrives without an ISBN — a converted file, some retail EPUBs —
// gives ingest nothing to join on, so the print entry a scan created stays
// `wanted` beside a fresh held book. The reviewer links them by hand; this
// picks the default and orders the list so the right entry is at the top.
// The normalisation mirrors the server's dedup package (case, accents,
// punctuation, a leading article, "Surname, Given") so a match here is one the
// server would have made had the ISBN been present.
import type { Book } from '@/api/types'
import type { InventoryEntry } from '@/api/client'

/** Entries still waiting for a book: not ingested and holding none. */
export function openEntries(rows: readonly InventoryEntry[]): InventoryEntry[] {
  return rows.filter((r) => r.state !== 'ingested' && r.book_id == null)
}

function fold(s: string): string {
  return s
    .replace(/&/g, ' and ')
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
    .replace(/\s+/g, ' ')
}

function normalizeAuthor(s: string): string {
  const parts = s.split(',')
  if (parts.length === 2 && parts[0]!.trim() && parts[1]!.trim()) {
    s = `${parts[1]!.trim()} ${parts[0]!.trim()}`
  }
  return fold(s)
}

function normalizeTitle(s: string): string {
  const t = fold(s)
  return t.replace(/^(the|a|an) (?=.)/, '')
}

/** 2 = same title, 1 = one title contains the other (a packager's "Series - Title"), 0 = no. */
function titleScore(book: Book, r: InventoryEntry): number {
  const a = normalizeTitle(book.title)
  const b = normalizeTitle(r.title ?? '')
  if (!a || !b) return 0
  if (a === b) return 2
  return a.includes(b) || b.includes(a) ? 1 : 0
}

function sameAuthor(book: Book, r: InventoryEntry): boolean {
  const a = normalizeAuthor(book.author)
  const b = normalizeAuthor(r.author ?? '')
  return !!a && a === b
}

function score(book: Book, r: InventoryEntry): number {
  return (sameAuthor(book, r) ? 4 : 0) + titleScore(book, r)
}

/** Open entries ordered for a held book: same author first, then matching titles, then the rest by title. */
export function rankWanted(book: Book, rows: readonly InventoryEntry[]): InventoryEntry[] {
  return [...rows].sort((x, y) => {
    const d = score(book, y) - score(book, x)
    if (d !== 0) return d
    return (x.title ?? x.isbn).localeCompare(y.title ?? y.isbn)
  })
}

/** The entry a held book almost certainly fulfils — same author and a matching title — else null. */
export function suggestWanted(book: Book, rows: readonly InventoryEntry[]): InventoryEntry | null {
  const best = rankWanted(book, rows)[0]
  if (!best) return null
  return sameAuthor(book, best) && titleScore(book, best) > 0 ? best : null
}
