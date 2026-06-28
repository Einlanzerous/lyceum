import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import LibraryView from './LibraryView.vue'
import { useLibraryStore } from '@/stores/library'
import type { Book } from '@/api/types'

const books: Book[] = [
  { id: 1, title: 'Dune', author: 'Herbert', cover_url: '/books/1/cover', progress: 0.42 },
  { id: 2, title: 'No Cover', author: 'Anon', cover_url: '' },
]

function mountView() {
  const wrapper = mount(LibraryView, {
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn, stubActions: true })],
      stubs: { RouterLink: RouterLinkStub },
    },
  })
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('LibraryView', () => {
  it('calls load() on mount', () => {
    const wrapper = mountView()
    const store = useLibraryStore()
    expect(store.load).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('renders a card per book and shows the progress badge only when known', async () => {
    const wrapper = mountView()
    const store = useLibraryStore()
    store.books = books
    store.loading = false
    await flushPromises()

    const cards = wrapper.findAllComponents(RouterLinkStub)
    // One card per book; each links to its reader route.
    const readerLinks = cards.filter((c) => String(c.props('to')).startsWith('/reader/'))
    expect(readerLinks).toHaveLength(2)
    expect(readerLinks[0]!.props('to')).toBe('/reader/1')

    // Progress badge only on the book that has progress.
    expect(wrapper.text()).toContain('42%')
    expect(wrapper.findAll('.book-card__progress')).toHaveLength(1)
  })

  it('shows an empty-state message when there are no books', async () => {
    const wrapper = mountView()
    const store = useLibraryStore()
    store.books = []
    store.loading = false
    await flushPromises()
    expect(wrapper.text()).toContain('No books yet')
  })

  it('ingests picked EPUBs and toasts a summary', async () => {
    const wrapper = mountView()
    const store = useLibraryStore()
    store.loading = false
    vi.mocked(store.uploadMany).mockResolvedValue([
      { kind: 'added', book: books[0]! },
      { kind: 'duplicate' },
    ])
    await flushPromises()

    const input = wrapper.find('input[type="file"]')
    const file = new File(['x'], 'a.epub', { type: 'application/epub+zip' })
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await flushPromises()

    expect(store.uploadMany).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('1 added')
    expect(wrapper.text()).toContain('1 already in your library')
  })
})
