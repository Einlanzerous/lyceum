import { defineStore } from 'pinia'
import { ApiError, listLibrary, setBookFinished, uploadBook } from '@/api/client'
import type { Book } from '@/api/types'

/** Outcome of an upload attempt, so the view can message each case distinctly. */
export type UploadResult =
  | { kind: 'added'; book: Book }
  | { kind: 'duplicate' }
  | { kind: 'error'; message: string }

interface LibraryState {
  books: Book[]
  loading: boolean
  error: string | null
}

export const useLibraryStore = defineStore('library', {
  state: (): LibraryState => ({
    books: [],
    loading: false,
    error: null,
  }),

  actions: {
    /** Load (or reload) the full library. */
    async load(): Promise<void> {
      this.loading = true
      this.error = null
      try {
        this.books = await listLibrary()
      } catch (err) {
        this.error = err instanceof Error ? err.message : 'failed to load library'
      } finally {
        this.loading = false
      }
    },

    /**
     * Upload one EPUB and fold the new book into the grid without a reload. A
     * 409 is an expected outcome (the book is already present), not an error.
     */
    async upload(file: File): Promise<UploadResult> {
      try {
        const book = await uploadBook(file)
        // Defensive: avoid a duplicate tile if the book somehow already shows.
        if (!this.books.some((b) => b.id === book.id)) {
          this.books = [book, ...this.books]
        }
        return { kind: 'added', book }
      } catch (err) {
        if (err instanceof ApiError && err.status === 409) {
          return { kind: 'duplicate' }
        }
        return { kind: 'error', message: err instanceof Error ? err.message : 'upload failed' }
      }
    },

    /**
     * Mark a book read/unread. Updates the local shelf optimistically and rolls
     * back if the server rejects it.
     */
    async setFinished(bookId: number, finished: boolean): Promise<void> {
      const book = this.books.find((b) => b.id === bookId)
      const prev = book?.finished
      if (book) book.finished = finished
      try {
        await setBookFinished(bookId, finished)
      } catch (err) {
        if (book) book.finished = prev
        throw err
      }
    },

    /** Upload several files, returning a result per file in input order. */
    async uploadMany(files: File[]): Promise<UploadResult[]> {
      const results: UploadResult[] = []
      for (const file of files) {
        results.push(await this.upload(file))
      }
      return results
    },
  },
})
