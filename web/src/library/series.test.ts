import { describe, expect, it } from 'vitest'
import { buildShelf, memberStatus, pinnedBookId, resumeIndex, type ShelfItem } from './series'
import type { Book } from '@/api/types'

function book(partial: Partial<Book> & { id: number }): Book {
  return {
    title: `Book ${partial.id}`,
    author: 'Anon',
    cover_url: '',
    ...partial,
  }
}

const defaultSort = { key: 'title', dir: 'asc' } as const

describe('memberStatus', () => {
  it('classifies by progress', () => {
    expect(memberStatus(book({ id: 1 }))).toBe('not-started')
    expect(memberStatus(book({ id: 2, progress: 0 }))).toBe('not-started')
    expect(memberStatus(book({ id: 3, progress: 0.4 }))).toBe('in-progress')
    expect(memberStatus(book({ id: 4, progress: 1 }))).toBe('finished')
    expect(memberStatus(book({ id: 5, progress: 0.995 }))).toBe('finished')
  })
})

describe('resumeIndex', () => {
  it('picks the furthest in-progress volume', () => {
    const members = [book({ id: 1, progress: 1 }), book({ id: 2, progress: 0.7 }), book({ id: 3 })]
    expect(resumeIndex(members)).toBe(1)
  })

  it('falls back to the first unstarted volume', () => {
    const members = [book({ id: 1, progress: 1 }), book({ id: 2 }), book({ id: 3 })]
    expect(resumeIndex(members)).toBe(1)
  })

  it('falls back to the first volume when everything is finished', () => {
    const members = [book({ id: 1, progress: 1 }), book({ id: 2, progress: 1 })]
    expect(resumeIndex(members)).toBe(0)
  })
})

describe('buildShelf', () => {
  it('rolls a ≥2 series into one card and keeps singletons loose', () => {
    const books = [
      book({ id: 1, title: 'Annihilation', series: 'Southern Reach', series_index: 1 }),
      book({ id: 2, title: 'Authority', series: 'Southern Reach', series_index: 2 }),
      book({ id: 3, title: 'Dune' }),
      book({ id: 4, title: 'Solo', series: 'Lonely', series_index: 1 }),
    ]
    const items = buildShelf(books, defaultSort)
    const series = items.filter(
      (i): i is Extract<ShelfItem, { kind: 'series' }> => i.kind === 'series',
    )
    expect(series).toHaveLength(1)
    expect(series[0]!.series.name).toBe('Southern Reach')
    expect(series[0]!.series.members).toHaveLength(2)
    // A single-book "series" stays a normal book card.
    const bookItems = items.filter((i) => i.kind === 'book')
    expect(bookItems).toHaveLength(2)
  })

  it('orders series members by series_index', () => {
    const books = [
      book({ id: 1, title: 'B', series: 'S', series_index: 2 }),
      book({ id: 2, title: 'A', series: 'S', series_index: 1 }),
      book({ id: 3, title: 'C', series: 'S', series_index: 3 }),
    ]
    const [item] = buildShelf(books, defaultSort)
    expect(item!.kind).toBe('series')
    if (item!.kind === 'series') {
      expect(item.series.members.map((m) => m.id)).toEqual([2, 1, 3])
    }
  })

  it('computes aggregate progress as the mean of members', () => {
    const books = [
      book({ id: 1, series: 'S', series_index: 1, progress: 1 }),
      book({ id: 2, series: 'S', series_index: 2, progress: 0.5 }),
      book({ id: 3, series: 'S', series_index: 3 }),
    ]
    const [item] = buildShelf(books, defaultSort)
    if (item!.kind === 'series') {
      expect(item.series.progress).toBeCloseTo(0.5, 5)
    }
  })

  it('groups case-insensitively', () => {
    const books = [
      book({ id: 1, series: 'The Expanse', series_index: 1 }),
      book({ id: 2, series: 'the expanse', series_index: 2 }),
    ]
    const items = buildShelf(books, defaultSort)
    expect(items).toHaveLength(1)
    expect(items[0]!.kind).toBe('series')
  })

  it('sorts shelf items by title ascending', () => {
    const books = [
      book({ id: 1, title: 'Zebra' }),
      book({ id: 2, title: 'Apple' }),
      book({ id: 3, title: 'Mango' }),
    ]
    const items = buildShelf(books, { key: 'title', dir: 'asc' })
    const titles = items.map((i) => (i.kind === 'book' ? i.book.title : i.series.name))
    expect(titles).toEqual(['Apple', 'Mango', 'Zebra'])
  })

  it('pins the current read to the front, floating its series card if grouped', () => {
    const books = [
      book({ id: 1, title: 'Apple' }),
      book({ id: 2, title: 'Boxed 1', series: 'Boxed', series_index: 1 }),
      book({
        id: 3,
        title: 'Boxed 2',
        series: 'Boxed',
        series_index: 2,
        progress: 0.5,
        read_at: '2026-05-01T00:00:00Z',
      }),
    ]
    const items = buildShelf(books, { key: 'title', dir: 'asc' }, 3)
    // The series holding the in-progress book #3 floats to the front.
    expect(items[0]!.kind).toBe('series')
    if (items[0]!.kind === 'series') expect(items[0].series.name).toBe('Boxed')
  })
})

describe('pinnedBookId', () => {
  it('returns the most-recently-read in-progress book', () => {
    const books = [
      book({ id: 1, progress: 0.3, read_at: '2026-01-01T00:00:00Z' }),
      book({ id: 2, progress: 0.6, read_at: '2026-05-01T00:00:00Z' }),
      book({ id: 3, progress: 1, read_at: '2026-06-01T00:00:00Z' }), // finished — ignored
      book({ id: 4 }), // never opened
    ]
    expect(pinnedBookId(books)).toBe(2)
  })

  it('returns null when nothing is mid-read', () => {
    expect(pinnedBookId([book({ id: 1 }), book({ id: 2, progress: 1, read_at: 'x' })])).toBeNull()
  })
})
