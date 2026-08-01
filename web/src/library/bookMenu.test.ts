import { describe, expect, it } from 'vitest'
import { bookMenuItems, isFinished } from './bookMenu'
import type { Book } from '@/api/types'

const book = (over: Partial<Book> = {}): Book => ({
  id: 1,
  title: 'Dune',
  author: 'Herbert',
  cover_url: '',
  ...over,
})

describe('bookMenuItems', () => {
  it('offers read/unread and a destructive remove', () => {
    const items = bookMenuItems(book())
    expect(items.map((i) => i.key)).toEqual(['finish', 'remove'])
    expect(items[1]!.danger).toBe(true)
  })

  it('labels the toggle from the finished flag', () => {
    expect(bookMenuItems(book({ finished: false }))[0]!.label).toBe('Mark as read')
    expect(bookMenuItems(book({ finished: true }))[0]!.label).toBe('Mark as unread')
  })

  // The series drawer used to label this from memberStatus(), which calls a book
  // at >=99% "finished" whether or not it is marked read. The menu writes the
  // `finished` flag, so labelling it from progress made the entry claim "Mark as
  // unread" and then write back false — the value the book already had, so
  // nothing changed and the label never flipped.
  it('ignores the progress heuristic: 99% but unread still offers "Mark as read"', () => {
    const nearlyDone = book({ progress: 0.995, finished: false })
    expect(isFinished(nearlyDone)).toBe(false)
    expect(bookMenuItems(nearlyDone)[0]!.label).toBe('Mark as read')
  })

  it('treats a missing finished flag as unread', () => {
    expect(isFinished(book())).toBe(false)
    expect(bookMenuItems(book())[0]!.label).toBe('Mark as read')
  })
})
