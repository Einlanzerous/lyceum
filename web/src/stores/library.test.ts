import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useLibraryStore } from './library'
import { ApiError } from '@/api/client'
import type { Book } from '@/api/types'

vi.mock('@/api/client', async () => {
  const actual = await vi.importActual<typeof import('@/api/client')>('@/api/client')
  return {
    ...actual,
    listLibrary: vi.fn(),
    uploadBook: vi.fn(),
  }
})

import { listLibrary, uploadBook } from '@/api/client'

const book = (id: number): Book => ({
  id,
  title: `Book ${id}`,
  author: 'Author',
  cover_url: `/books/${id}/cover`,
})

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('library store', () => {
  it('load() populates books', async () => {
    vi.mocked(listLibrary).mockResolvedValue([book(1), book(2)])
    const store = useLibraryStore()
    await store.load()
    expect(store.books).toHaveLength(2)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })

  it('load() records an error message on failure', async () => {
    vi.mocked(listLibrary).mockRejectedValue(new ApiError(500, 'boom'))
    const store = useLibraryStore()
    await store.load()
    expect(store.error).toBe('boom')
    expect(store.books).toEqual([])
  })

  it('upload() prepends the new book without a reload', async () => {
    vi.mocked(uploadBook).mockResolvedValue(book(9))
    const store = useLibraryStore()
    store.books = [book(1)]
    const result = await store.upload(new File(['x'], 'a.epub'))
    expect(result).toEqual({ kind: 'added', book: book(9) })
    expect(store.books.map((b) => b.id)).toEqual([9, 1])
  })

  it('upload() maps a 409 to a duplicate result (not an error)', async () => {
    vi.mocked(uploadBook).mockRejectedValue(new ApiError(409, 'book already exists'))
    const store = useLibraryStore()
    const result = await store.upload(new File(['x'], 'dupe.epub'))
    expect(result).toEqual({ kind: 'duplicate' })
    expect(store.books).toEqual([])
  })

  it('upload() reports other failures as errors', async () => {
    vi.mocked(uploadBook).mockRejectedValue(new ApiError(400, 'not an epub'))
    const store = useLibraryStore()
    const result = await store.upload(new File(['x'], 'bad.epub'))
    expect(result).toEqual({ kind: 'error', message: 'not an epub' })
  })
})
