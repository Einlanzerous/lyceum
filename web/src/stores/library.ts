import { defineStore } from 'pinia'
import { ApiError, deleteBook, listLibrary, setBookFinished, uploadBook } from '@/api/client'
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

    /**
     * Remove a book from the library for good: the row, its blobs, and every
     * reading position go with it (LYCM-109). The tile is dropped optimistically
     * and put back at its old place if the server refuses, so a failed delete
     * doesn't silently lose the book from the shelf until the next reload.
     */
    async remove(bookId: number): Promise<void> {
      const index = this.books.findIndex((b) => b.id === bookId)
      if (index === -1) return
      const [removed] = this.books.splice(index, 1)
      try {
        await deleteBook(bookId)
      } catch (err) {
        // The shelf may have been replaced while the request was in flight (a
        // load() landing, another tile going). Only put the book back if it is
        // genuinely absent, and clamp the old index — blindly splicing at a
        // stale position can duplicate it or drop it in the wrong slot.
        if (!this.books.some((b) => b.id === bookId)) {
          this.books.splice(Math.min(index, this.books.length), 0, removed!)
        }
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
