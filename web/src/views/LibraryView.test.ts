import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import LibraryView from './LibraryView.vue'
import { useLibraryStore } from '@/stores/library'
import { __resetServerCache, __setNativeShell } from '@/api/base'
import { useServer } from '@/api/useServer'
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

afterEach(() => {
  // Reset the shared server ref + native override so tests don't leak state.
  useServer().save('')
  __setNativeShell(null)
  __resetServerCache()
  localStorage.clear()
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
    expect(wrapper.findAll('.card__pill')).toHaveLength(1)
  })

  it('shows an empty-state message when there are no books', async () => {
    const wrapper = mountView()
    const store = useLibraryStore()
    store.books = []
    store.loading = false
    await flushPromises()
    expect(wrapper.text()).toContain('No books yet')
  })

  it('native shell: prompts to connect and skips load until a server is set', async () => {
    __setNativeShell(true)
    const wrapper = mountView()
    const store = useLibraryStore()
    // No backend yet → don't fetch the library, show the connect prompt.
    expect(store.load).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Connect to your library')
    wrapper.unmount()
  })

  it('native shell: saving a server hides the prompt and loads the library', async () => {
    __setNativeShell(true)
    const wrapper = mountView()
    const store = useLibraryStore()

    await wrapper.find('#server-url').setValue('http://home.lan:8080')
    await wrapper.find('.conn__btn--brass').trigger('click')
    await flushPromises()

    expect(store.load).toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('Connect to your library')
    wrapper.unmount()
  })
})
