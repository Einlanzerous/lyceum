import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import EditBookDialog from './EditBookDialog.vue'
import type { Book } from '@/api/types'

const book: Book = {
  id: 7,
  title: 'The Well of Ascension',
  author: 'Brandon Sanderson',
  cover_url: '',
}

function inputs(w: ReturnType<typeof mount>) {
  const all = w.findAll('input')
  return { title: all[0]!, author: all[1]!, series: all[2]!, index: all[3]! }
}

describe('EditBookDialog', () => {
  it('starts from the book and saves a series with its number (LYCM-129)', async () => {
    const w = mount(EditBookDialog, { props: { book } })
    const f = inputs(w)
    expect((f.title.element as HTMLInputElement).value).toBe('The Well of Ascension')
    expect((f.series.element as HTMLInputElement).value).toBe('')

    await f.series.setValue('Mistborn')
    await f.index.setValue('2')
    await w.find('form').trigger('submit')

    expect(w.emitted('save')).toEqual([
      [
        7,
        {
          title: 'The Well of Ascension',
          author: 'Brandon Sanderson',
          series: 'Mistborn',
          series_index: 2,
        },
      ],
    ])
  })

  it('refuses to save without a title', async () => {
    const w = mount(EditBookDialog, { props: { book } })
    await inputs(w).title.setValue('   ')
    expect((w.find('button[type=submit]').element as HTMLButtonElement).disabled).toBe(true)
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toBeUndefined()
  })

  it('closes on Cancel, Escape and a click on the scrim', async () => {
    const w = mount(EditBookDialog, { props: { book }, attachTo: document.body })
    await w.find('button[type=button]').trigger('click')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await w.find('.scrim').trigger('click')
    expect(w.emitted('close')).toHaveLength(3)
    w.unmount()
  })
})
