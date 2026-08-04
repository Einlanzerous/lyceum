import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ReviewView from './ReviewView.vue'
import * as client from '@/api/client'
import type { Book } from '@/api/types'

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof client>()),
  listPendingReview: vi.fn(),
  getBook: vi.fn(),
  approveBook: vi.fn(),
  deleteBook: vi.fn(),
}))

// RouterLink is the only router surface this view uses.
const global = { stubs: { RouterLink: { template: '<a><slot /></a>' } } }

const onShelf: Book = {
  id: 1,
  title: 'Piranesi',
  author: 'Susanna Clarke',
  cover_url: '/books/1/cover',
}

const held: Book = {
  id: 2,
  title: 'Piranesi',
  author: 'Clarke, Susanna',
  cover_url: '/books/2/cover',
  review_state: 'pending',
  review_flags: ['possible_duplicate'],
  duplicate_of: 1,
}

describe('ReviewView duplicate decision (LYCM-113)', () => {
  beforeEach(() => {
    vi.mocked(client.listPendingReview).mockReset()
    vi.mocked(client.getBook).mockReset()
    vi.mocked(client.approveBook)
      .mockReset()
      .mockResolvedValue(undefined as never)
    vi.mocked(client.deleteBook)
      .mockReset()
      .mockResolvedValue(undefined as never)
  })

  it('shows the book it matched, so the two can be compared', async () => {
    vi.mocked(client.listPendingReview).mockResolvedValue([held])
    vi.mocked(client.getBook).mockResolvedValue(onShelf)

    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    expect(client.getBook).toHaveBeenCalledWith(1)
    const text = wrapper.text()
    expect(text).toContain('another copy of a book you already have')
    expect(text).toContain('Already on the shelf')
    // Both authors render, which is the difference a person is deciding on.
    expect(text).toContain('Susanna Clarke')
    expect(text).toContain('Clarke, Susanna')
  })

  it('frames the actions as a choice about the pair, not a quality judgement', async () => {
    vi.mocked(client.listPendingReview).mockResolvedValue([held])
    vi.mocked(client.getBook).mockResolvedValue(onShelf)

    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    const labels = wrapper.findAll('button').map((b) => b.text())
    expect(labels).toContain('Keep both')
    expect(labels).toContain('Delete this copy')
    expect(labels).not.toContain('Approve')
  })

  it('keeps the ordinary labels for a book held on a quality flag', async () => {
    vi.mocked(client.listPendingReview).mockResolvedValue([
      {
        id: 3,
        title: 'Odd',
        author: '',
        cover_url: '',
        review_state: 'pending',
        review_flags: ['no_isbn'],
      },
    ])

    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    const labels = wrapper.findAll('button').map((b) => b.text())
    expect(labels).toContain('Approve')
    expect(labels).toContain('Delete')
    expect(wrapper.text()).not.toContain('another copy of a book you already have')
    // No counterpart to fetch, so the view must not go asking for one.
    expect(client.getBook).not.toHaveBeenCalled()
  })

  it('says so when the matched book has already been deleted', async () => {
    vi.mocked(client.listPendingReview).mockResolvedValue([held])
    vi.mocked(client.getBook).mockRejectedValue(new Error('book not found'))

    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    expect(wrapper.text()).toContain('has since been deleted')
  })

  it('still explains the hold once the server has dropped the pointer', async () => {
    // The real steady state after the match is deleted: duplicate_of is nulled
    // by the FK and omitted from the wire, leaving only the flag. Gating the
    // panel on the pointer hid it exactly here.
    const orphaned: Book = { ...held }
    delete orphaned.duplicate_of
    vi.mocked(client.listPendingReview).mockResolvedValue([orphaned])

    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    expect(client.getBook).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('has since been deleted')
    expect(wrapper.findAll('button').map((b) => b.text())).toContain('Keep both')
  })

  it('fetches a shared counterpart once, however many rows point at it', async () => {
    const second: Book = { ...held, id: 4, author: 'S. Clarke' }
    vi.mocked(client.listPendingReview).mockResolvedValue([held, second])
    vi.mocked(client.getBook).mockResolvedValue(onShelf)

    mount(ReviewView, { global })
    await flushPromises()

    expect(vi.mocked(client.getBook).mock.calls).toEqual([[1]])
  })

  it('renders the queue even while the counterpart is still loading', async () => {
    vi.mocked(client.listPendingReview).mockResolvedValue([held])
    // Never resolves: the comparison must not hold the whole queue behind it.
    vi.mocked(client.getBook).mockReturnValue(new Promise<Book>(() => {}))

    const wrapper = mount(ReviewView, { global })
    await flushPromises()

    // The row itself is fully interactive — its edit fields are populated and
    // its buttons are there — with only the comparison panel still pending.
    const title = wrapper.find('input[type="text"]').element as HTMLInputElement
    expect(title.value).toBe('Piranesi')
    expect(wrapper.findAll('button').map((b) => b.text())).toContain('Keep both')
    expect(wrapper.text()).toContain('Loading the other copy…')
  })
})
