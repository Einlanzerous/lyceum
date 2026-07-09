import { afterEach, describe, expect, it } from 'vitest'
import { defaultDir, loadSort, saveSort, sortBooks, type SortState } from './sort'
import type { Book } from '@/api/types'

function book(partial: Partial<Book> & { id: number }): Book {
  return { title: `Book ${partial.id}`, author: 'Anon', cover_url: '', ...partial }
}

afterEach(() => localStorage.clear())

describe('defaultDir', () => {
  it('is newest-first for dates and A→Z for text', () => {
    expect(defaultDir('added')).toBe('desc')
    expect(defaultDir('title')).toBe('asc')
    expect(defaultDir('author')).toBe('asc')
  })
})

describe('sortBooks', () => {
  const books = [
    book({ id: 1, title: 'Mango', author: 'Clarke', added_at: '2026-01-02T00:00:00Z' }),
    book({ id: 2, title: 'apple', author: 'Adams', added_at: '2026-03-01T00:00:00Z' }),
    book({ id: 3, title: 'Zebra', author: 'Zola', added_at: '2026-02-01T00:00:00Z' }),
  ]

  it('sorts by title case-insensitively', () => {
    const asc = sortBooks(books, { key: 'title', dir: 'asc' }).map((b) => b.id)
    expect(asc).toEqual([2, 1, 3]) // apple, Mango, Zebra
    const desc = sortBooks(books, { key: 'title', dir: 'desc' }).map((b) => b.id)
    expect(desc).toEqual([3, 1, 2])
  })

  it('sorts by author', () => {
    const asc = sortBooks(books, { key: 'author', dir: 'asc' }).map((b) => b.id)
    expect(asc).toEqual([2, 1, 3]) // Adams, Clarke, Zola
  })

  it('sorts by recently added using added_at', () => {
    const desc = sortBooks(books, { key: 'added', dir: 'desc' }).map((b) => b.id)
    expect(desc).toEqual([2, 3, 1]) // Mar, Feb, Jan
  })

  it('does not mutate the input array', () => {
    const input = [...books]
    sortBooks(input, { key: 'title', dir: 'asc' })
    expect(input.map((b) => b.id)).toEqual([1, 2, 3])
  })
})

describe('sort persistence', () => {
  it('defaults to newest-first when nothing is saved', () => {
    expect(loadSort()).toEqual({ key: 'added', dir: 'desc' })
  })

  it('round-trips a saved sort', () => {
    const state: SortState = { key: 'author', dir: 'desc' }
    saveSort(state)
    expect(loadSort()).toEqual(state)
  })

  it('ignores corrupt storage', () => {
    localStorage.setItem('lyceum.library.sort', 'not json')
    expect(loadSort()).toEqual({ key: 'added', dir: 'desc' })
  })
})
