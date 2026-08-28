import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SeriesDrawer from './SeriesDrawer.vue'
import { buildShelf, type SeriesGroup } from '@/library/series'
import type { Book } from '@/api/types'

function book(partial: Partial<Book> & { id: number }): Book {
  return { title: `Book ${partial.id}`, author: 'Anon', cover_url: '', ...partial }
}

function seriesOf(books: Book[]): SeriesGroup {
  const item = buildShelf(books, { key: 'title', dir: 'asc' })[0]!
  if (item.kind !== 'series') throw new Error('expected a series card')
  return item.series
}

function mountDrawer(series: SeriesGroup) {
  return mount(SeriesDrawer, {
    props: { series, arrowLeftPct: 50 },
    global: { stubs: { RouterLink: { props: ['to'], template: '<a><slot /></a>' } } },
  })
}

describe('SeriesDrawer volume numbers', () => {
  // Before LYCM-130 the label was the v-for index + 1, so an owned series with
  // gaps read Book 1, 2, 3, 4 regardless of which volumes were actually there.
  it('labels members by series_index, so gaps stay visible', () => {
    const drawer = mountDrawer(
      seriesOf([1, 4, 5, 7].map((n) => book({ id: n, series: 'HP', series_index: n }))),
    )
    const labels = drawer.findAll('.drawer__status').map((s) => s.text())
    expect(labels).toEqual([
      'Not started · Book 1',
      'Not started · Book 4',
      'Not started · Book 5',
      'Not started · Book 7',
    ])
    expect(drawer.find('.drawer__resume').text()).toBe('▸ Resume book 1')
  })

  it('resumes into the volume you are on, numbered by its series_index', () => {
    const drawer = mountDrawer(
      seriesOf([
        book({ id: 1, series: 'HP', series_index: 1, progress: 1 }),
        book({ id: 4, series: 'HP', series_index: 4, progress: 0.3 }),
      ]),
    )
    expect(drawer.find('.drawer__resume').text()).toBe('▸ Resume book 4')
  })

  it('shows a novella index as given', () => {
    const drawer = mountDrawer(
      seriesOf([
        book({ id: 1, series: 'Expanse', series_index: 3 }),
        book({ id: 2, series: 'Expanse', series_index: 3.5 }),
      ]),
    )
    expect(drawer.findAll('.drawer__status').map((s) => s.text())).toEqual([
      'Not started · Book 3',
      'Not started · Book 3.5',
    ])
  })

  it('leaves an unindexed volume unnumbered instead of inventing a position', () => {
    const drawer = mountDrawer(
      seriesOf([
        book({ id: 1, series: 'Loose', series_index: 1 }),
        book({ id: 2, series: 'Loose' }),
      ]),
    )
    expect(drawer.findAll('.drawer__status').map((s) => s.text())).toEqual([
      'Not started · Book 1',
      'Not started',
    ])
  })

  it('drops the number from Resume when the target volume has none', () => {
    const drawer = mountDrawer(
      seriesOf([book({ id: 1, series: 'Loose' }), book({ id: 2, series: 'Loose' })]),
    )
    expect(drawer.find('.drawer__resume').text()).toBe('▸ Resume')
  })
})
