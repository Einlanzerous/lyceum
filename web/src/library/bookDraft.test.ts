import { describe, expect, it } from 'vitest'
import { applySaved, draftOf, patchOf } from './bookDraft'
import type { Book } from '@/api/types'

const book = (over: Partial<Book> = {}): Book => ({
  id: 1,
  title: 'The Final Empire',
  author: 'Brandon Sanderson',
  cover_url: '',
  ...over,
})

describe('draftOf', () => {
  it('renders the series index as typed text and a missing one as blank', () => {
    expect(draftOf(book({ series: 'Mistborn', series_index: 1 }))).toEqual({
      title: 'The Final Empire',
      author: 'Brandon Sanderson',
      series: 'Mistborn',
      seriesIndex: '1',
    })
    expect(draftOf(book())).toMatchObject({ series: '', seriesIndex: '' })
    expect(draftOf(book({ series: 'Expanse', series_index: 3.5 })).seriesIndex).toBe('3.5')
  })
})

describe('patchOf', () => {
  it('sends the series and its parsed index', () => {
    expect(
      patchOf({
        title: ' The Final Empire ',
        author: ' Sanderson',
        series: 'Mistborn ',
        seriesIndex: ' 1 ',
      }),
    ).toEqual({
      title: 'The Final Empire',
      author: 'Sanderson',
      series: 'Mistborn',
      series_index: 1,
    })
  })

  it('keeps a novella index as given', () => {
    expect(
      patchOf({ title: 'T', author: '', series: 'Expanse', seriesIndex: '3.5' }).series_index,
    ).toBe(3.5)
  })

  it('sends series "" to clear, with no index', () => {
    // A cleared series field must reach the server as '' — leaving it out would
    // keep the old series.
    expect(patchOf({ title: 'T', author: 'A', series: '  ', seriesIndex: '4' })).toEqual({
      title: 'T',
      author: 'A',
      series: '',
      series_index: 0,
    })
  })

  it('treats a blank, junk or negative index as none', () => {
    for (const idx of ['', 'abc', '-2']) {
      expect(patchOf({ title: 'T', author: 'A', series: 'S', seriesIndex: idx }).series_index).toBe(
        0,
      )
    }
  })
})

describe('applySaved', () => {
  it('copies the editable fields, including a cleared series', () => {
    const target = book({ series: 'Mistborn', series_index: 1 })
    applySaved(target, book({ title: 'Renamed' }))
    expect(target).toMatchObject({ title: 'Renamed', series: undefined, series_index: undefined })
  })
})
