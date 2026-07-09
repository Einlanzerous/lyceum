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

const seriesBooks: Book[] = [
  { id: 1, title: 'Annihilation', author: 'VanderMeer', cover_url: '', series: 'Southern Reach', series_index: 1, progress: 1 },
  { id: 2, title: 'Authority', author: 'VanderMeer', cover_url: '', series: 'Southern Reach', series_index: 2, progress: 0.73 },
  { id: 3, title: 'Piranesi', author: 'Clarke', cover_url: '' },
]

function mountView(attach = false) {
  const wrapper = mount(LibraryView, {
    attachTo: attach ? document.body : undefined,
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
    // One card per book; each links to its reader route (order is sort-dependent).
    const readerLinks = cards.filter((c) => String(c.props('to')).startsWith('/reader/'))
    expect(readerLinks).toHaveLength(2)
    const targets = readerLinks.map((c) => String(c.props('to')))
    expect(targets).toContain('/reader/1')
    expect(targets).toContain('/reader/2')

    // Progress badge only on the book that has progress.
    expect(wrapper.text()).toContain('42%')
    expect(wrapper.findAll('.card__pill')).toHaveLength(1)
  })

  it('rolls a multi-book series into one card and expands an inline drawer', async () => {
    const wrapper = mountView()
    const store = useLibraryStore()
    store.books = seriesBooks
    store.loading = false
    await flushPromises()

    // The 3 books collapse to 2 tiles: the series card + the standalone.
    const seriesCard = wrapper.find('.series')
    expect(seriesCard.exists()).toBe(true)
    expect(wrapper.find('.drawer').exists()).toBe(false)

    await seriesCard.trigger('click')
    await flushPromises()

    // Drawer opens with both members and a resume shortcut.
    const drawer = wrapper.find('.drawer')
    expect(drawer.exists()).toBe(true)
    expect(drawer.text()).toContain('Annihilation')
    expect(drawer.text()).toContain('Authority')
    // Book 1 is finished, book 2 in progress → resume points at book 2.
    expect(drawer.text()).toContain('Resume book 2')

    // Clicking again collapses it.
    await wrapper.find('.series').trigger('click')
    await flushPromises()
    expect(wrapper.find('.drawer').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens a search overlay and filters the shelf by title', async () => {
    const wrapper = mountView(true)
    const store = useLibraryStore()
    store.books = seriesBooks
    store.loading = false
    await flushPromises()

    await wrapper.find('.lib__search-btn').trigger('click')
    await flushPromises()

    const input = document.body.querySelector<HTMLInputElement>('.search__input')
    expect(input).not.toBeNull()

    input!.value = 'piranesi'
    input!.dispatchEvent(new Event('input'))
    await flushPromises()

    // Only the matching book remains on the shelf.
    expect(wrapper.text()).toContain('Piranesi')
    expect(wrapper.text()).not.toContain('Annihilation')
    wrapper.unmount()
  })

  it('shows a no-match state when the search finds nothing', async () => {
    const wrapper = mountView(true)
    const store = useLibraryStore()
    store.books = seriesBooks
    store.loading = false
    await flushPromises()

    await wrapper.find('.lib__search-btn').trigger('click')
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>('.search__input')
    input!.value = 'zzznope'
    input!.dispatchEvent(new Event('input'))
    await flushPromises()

    expect(wrapper.text()).toContain('No matches')
    wrapper.unmount()
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
