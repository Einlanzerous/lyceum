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
    deleteBook: vi.fn(),
  }
})

import { deleteBook, listLibrary, uploadBook } from '@/api/client'

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
    expect(result).toMatchObject({ kind: 'duplicate' })
    expect(store.books).toEqual([])
  })

  it('remove() drops the tile and calls the API', async () => {
    vi.mocked(deleteBook).mockResolvedValue(undefined)
    const store = useLibraryStore()
    store.books = [book(1), book(2), book(3)]
    await store.remove(2)
    expect(deleteBook).toHaveBeenCalledWith(2)
    expect(store.books.map((b) => b.id)).toEqual([1, 3])
  })

  it('remove() restores the book at its old position when the server refuses', async () => {
    vi.mocked(deleteBook).mockRejectedValue(new ApiError(500, 'nope'))
    const store = useLibraryStore()
    store.books = [book(1), book(2), book(3)]
    await expect(store.remove(2)).rejects.toThrow('nope')
    expect(store.books.map((b) => b.id)).toEqual([1, 2, 3])
  })

  it('remove() does not duplicate the book when a reload lands mid-flight', async () => {
    // The shelf can be replaced while the DELETE is in flight. Rolling back by
    // splicing at the pre-await index would then re-insert a book the reload
    // already restored, or drop it in the wrong slot.
    let reject: (e: Error) => void = () => {}
    vi.mocked(deleteBook).mockReturnValue(
      new Promise<void>((_, rej) => {
        reject = rej
      }),
    )
    const store = useLibraryStore()
    store.books = [book(1), book(2), book(3)]

    const pending = store.remove(2)
    expect(store.books.map((b) => b.id)).toEqual([1, 3])

    // A concurrent load() brings the full shelf back, book 2 included.
    store.books = [book(1), book(2), book(3)]
    reject(new ApiError(500, 'nope'))
    await expect(pending).rejects.toThrow('nope')

    expect(store.books.map((b) => b.id)).toEqual([1, 2, 3])
  })

  it('remove() ignores an unknown id', async () => {
    const store = useLibraryStore()
    store.books = [book(1)]
    await store.remove(99)
    expect(deleteBook).not.toHaveBeenCalled()
    expect(store.books.map((b) => b.id)).toEqual([1])
  })

  it('upload() reports other failures as errors', async () => {
    vi.mocked(uploadBook).mockRejectedValue(new ApiError(400, 'not an epub'))
    const store = useLibraryStore()
    const result = await store.upload(new File(['x'], 'bad.epub'))
    expect(result).toEqual({ kind: 'error', message: 'not an epub' })
  })
})
