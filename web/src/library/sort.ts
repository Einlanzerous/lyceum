// Library sort order (LYCM-62). The whole shelf is loaded at once at personal
// scale, so sorting is done client-side; the chosen order is remembered in
// localStorage until per-user prefs land (LYCM-47).
import type { Book } from '@/api/types'

export type SortKey = 'added' | 'title' | 'author'
export type SortDir = 'asc' | 'desc'

export interface SortState {
  key: SortKey
  dir: SortDir
}

export const SORT_OPTIONS: ReadonlyArray<{ key: SortKey; label: string }> = [
  { key: 'added', label: 'Recently added' },
  { key: 'title', label: 'Title' },
  { key: 'author', label: 'Author' },
]

const STORAGE_KEY = 'lyceum.library.sort'

/** The natural first direction for a key: newest-first for dates, A→Z for text. */
export function defaultDir(key: SortKey): SortDir {
  return key === 'added' ? 'desc' : 'asc'
}

function isSortKey(v: unknown): v is SortKey {
  return v === 'added' || v === 'title' || v === 'author'
}

/** Read the saved sort, falling back to newest-first. */
export function loadSort(): SortState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<SortState>
      if (isSortKey(parsed.key) && (parsed.dir === 'asc' || parsed.dir === 'desc')) {
        return { key: parsed.key, dir: parsed.dir }
      }
    }
  } catch {
    // Corrupt/unavailable storage falls through to the default.
  }
  return { key: 'added', dir: 'desc' }
}

/** Persist the sort; storage failures are non-fatal (private mode, quota). */
export function saveSort(state: SortState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // ignore
  }
}

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true })

/** Compare by title (A→Z), tie-broken by id for a stable order. */
function byTitle(a: Book, b: Book): number {
  return collator.compare(a.title, b.title) || a.id - b.id
}

/**
 * Compare two shelf sort-fields by the given key. Callers pass the field values
 * they extracted (a series card sorts by its series name / newest member), so
 * this works for both loose books and rolled-up series.
 */
export function compareFields(
  key: SortKey,
  a: { title: string; author: string; added: string; id: number },
  b: { title: string; author: string; added: string; id: number },
): number {
  switch (key) {
    case 'title':
      return collator.compare(a.title, b.title) || a.id - b.id
    case 'author':
      // Books with no author sink below named ones, then fall back to title.
      if (!a.author !== !b.author) return a.author ? -1 : 1
      return (
        collator.compare(a.author, b.author) || collator.compare(a.title, b.title) || a.id - b.id
      )
    case 'added':
      // added_at is RFC3339 UTC, so lexical compare is chronological.
      return a.added.localeCompare(b.added) || a.id - b.id
  }
}

/** Sort a flat list of books by the given state (used for search results). */
export function sortBooks(books: readonly Book[], state: SortState): Book[] {
  const sorted = [...books].sort((a, b) => compareFields(state.key, fieldsOf(a), fieldsOf(b)))
  if (state.dir === 'desc') sorted.reverse()
  return sorted
}

/** Extract the comparable fields from a book. */
export function fieldsOf(b: Book): { title: string; author: string; added: string; id: number } {
  return { title: b.title, author: b.author, added: b.added_at ?? '', id: b.id }
}

// byTitle is exported for the series member ordering fallback.
export { byTitle }
