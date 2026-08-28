// Which wanted inventory entry a held book most likely fulfils (LYCM-128).
//
// An EPUB that arrives without an ISBN — a converted file, some retail EPUBs —
// gives ingest nothing to join on, so the print entry a scan created stays
// `wanted` beside a fresh held book. The reviewer links them by hand; this
// picks the default and orders the list so the right entry is at the top.
//
// The normalisation mirrors the server's dedup package (case, accents,
// punctuation, a leading article, "Hobbit, The", "Surname, Given"), and the
// *default* is as strict as the server's own fallback — an exact normalised
// title match — with one allowance the server cannot make: a packager's series
// prefix ("Mistborn - The Final Empire") is stripped before comparing. Looser
// containment only orders the list; it never selects, because series volumes
// share an author and a title stem ("Dune" / "Dune Messiah") and a wrong link
// takes a click to make and a delete to undo.
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

/** "Hobbit, The" → "The Hobbit", the cataloguing inversion dedup also undoes. */
function uninvertArticle(s: string): string {
  const i = s.lastIndexOf(',')
  if (i < 0) return s
  const head = s.slice(0, i).trim()
  const tail = s.slice(i + 1).trim()
  if (head && /^(the|a|an)$/i.test(tail)) return `${tail} ${head}`
  return s
}

function normalizeTitle(s: string): string {
  return fold(uninvertArticle(s)).replace(/^(the|a|an) (?=.)/, '')
}

/**
 * The titles a held book's title could be standing in for: itself, and — when
 * a packager wrote "Series - Title" or "Series: Title" — the part after the
 * separator. Only a *trailing* segment qualifies: the series name comes first
 * in every such convention, and taking the leading segment too would let
 * "Dune: Part Two" match "Dune".
 */
function titleVariants(title: string): string[] {
  const out = [normalizeTitle(title)]
  const m = /^(.+?)\s+[-–—:]\s+(.+)$/.exec(title.trim())
  if (m) out.push(normalizeTitle(m[2]!))
  return out.filter((t) => t)
}

/** 2 = the entry's title is (a variant of) the book's; 1 = one contains the other; 0 = no. */
function titleScore(book: Book, r: InventoryEntry): number {
  const b = normalizeTitle(r.title ?? '')
  if (!b) return 0
  const variants = titleVariants(book.title)
  if (variants.length === 0) return 0
  if (variants.includes(b)) return 2
  const a = variants[0]!
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

/**
 * The entry a held book almost certainly fulfils — same author and an exact
 * (series-prefix-stripped) title match — else null. A containment-only match
 * is offered in the list but never preselected.
 */
export function suggestWanted(book: Book, rows: readonly InventoryEntry[]): InventoryEntry | null {
  const best = rankWanted(book, rows)[0]
  if (!best) return null
  return sameAuthor(book, best) && titleScore(book, best) === 2 ? best : null
}
