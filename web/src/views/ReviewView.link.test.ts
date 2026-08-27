import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ReviewView from './ReviewView.vue'
import * as client from '@/api/client'
import type { Book } from '@/api/types'
import type { InventoryEntry } from '@/api/client'

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof client>()),
  listPendingReview: vi.fn(),
  listInventory: vi.fn(),
  linkBookToInventory: vi.fn(),
}))

const global = { stubs: { RouterLink: { template: '<a><slot /></a>' } } }

// A calibre-converted azw3: no ISBN, the packager's "Series - Title" title,
// the author inverted.
const held: Book = {
  id: 2,
  title: 'Mistborn - The Final Empire',
  author: 'Sanderson, Brandon',
  cover_url: '',
  review_state: 'pending',
  review_flags: ['no_isbn'],
}

const finalEmpire: InventoryEntry = {
  id: 7,
  isbn: '9780765311788',
  title: 'The Final Empire',
  author: 'Brandon Sanderson',
  state: 'wanted',
}
const dune: InventoryEntry = {
  id: 8,
  isbn: '9780441172719',
  title: 'Dune',
  author: 'Frank Herbert',
  state: 'wanted',
}
const alreadyIn: InventoryEntry = {
  id: 9,
  isbn: '9780000000009',
  title: 'Elantris',
  author: 'Brandon Sanderson',
  state: 'ingested',
  book_id: 5,
}

describe('ReviewView — fulfil a wanted title (LYCM-128)', () => {
  beforeEach(() => {
    vi.mocked(client.listPendingReview).mockReset().mockResolvedValue([held])
    vi.mocked(client.listInventory).mockReset().mockResolvedValue([dune, alreadyIn, finalEmpire])
    vi.mocked(client.linkBookToInventory)
      .mockReset()
      .mockResolvedValue({
        book: { ...held, series: 'Mistborn', series_index: 1 },
        inventory: { ...finalEmpire, state: 'ingested', book_id: 2 },
      })
  })

  it('offers the open entries with the likely match preselected, and links it', async () => {
    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    const select = wrapper.find('select.link__select')
    expect(select.exists()).toBe(true)
    // Only entries still waiting for a book are offered, best match first.
    const options = select.findAll('option').map((o) => o.text())
    expect(options[0]).toBe('— none —')
    expect(options[1]).toContain('The Final Empire')
    expect(options[2]).toContain('Dune')
    expect(options).toHaveLength(3) // Elantris is already ingested
    expect((select.element as HTMLSelectElement).value).toBe('7')

    await wrapper.find('.link .btn').trigger('click')
    await flushPromises()

    expect(client.linkBookToInventory).toHaveBeenCalledWith(2, 7)
    expect(wrapper.text()).toContain('Fulfils “The Final Empire”')
    expect(wrapper.find('select.link__select').exists()).toBe(false)
  })

  it('clears the same suggestion from other cards once the entry is taken', async () => {
    // Two conversions of the same book both suggest entry 7. Linking one must
    // not leave the other with a blank select and a live Link button.
    const twin: Book = { ...held, id: 3 }
    vi.mocked(client.listPendingReview).mockResolvedValue([held, twin])
    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    const selects = wrapper.findAll('select.link__select')
    expect(selects.map((s) => (s.element as HTMLSelectElement).value)).toEqual(['7', '7'])

    await wrapper.findAll('.link .btn')[0]!.trigger('click')
    await flushPromises()

    const remaining = wrapper.find('select.link__select')
    expect((remaining.element as HTMLSelectElement).value).toBe('0')
    expect(
      remaining
        .findAll('option')
        .map((o) => o.text())
        .join(' '),
    ).not.toContain('The Final Empire')
    expect((wrapper.find('.link .btn').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('leaves the button off until an entry is picked', async () => {
    vi.mocked(client.listInventory).mockResolvedValue([dune])
    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    const select = wrapper.find('select.link__select')
    expect((select.element as HTMLSelectElement).value).toBe('0') // Dune is not a match
    expect((wrapper.find('.link .btn').element as HTMLButtonElement).disabled).toBe(true)
    await select.setValue('8')
    expect((wrapper.find('.link .btn').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('shows the queue without the control when inventory cannot be read', async () => {
    vi.mocked(client.listInventory).mockRejectedValue(new Error('boom'))
    const wrapper = mount(ReviewView, { global })
    await flushPromises()
    // The card is there (its title is the first field's value)...
    expect((wrapper.find('input.field__input').element as HTMLInputElement).value).toBe(
      'Mistborn - The Final Empire',
    )
    // ...just without the link control.
    expect(wrapper.find('select.link__select').exists()).toBe(false)
  })
})
