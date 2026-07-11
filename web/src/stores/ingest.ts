import { defineStore } from 'pinia'
import {
  addCandidate,
  confirmCandidate,
  confirmReady,
  createBatch,
  getBatch,
  listBatches,
  pickEdition,
  searchEditions,
  skipCandidate,
  type Batch,
  type Candidate,
  type CandidateStatus,
  type Edition,
  type ScanSource,
} from '@/api/ingest'

/** Statuses that still need the reviewer: everything not yet confirmed/skipped. */
const REVIEWABLE: CandidateStatus[] = ['ready', 'review', 'no_match', 'duplicate']

export type QueueFilter = 'all' | 'ready' | 'review' | 'no_match'

interface IngestState {
  batches: Batch[]
  batch: Batch | null
  selectedId: number | null
  filter: QueueFilter
  search: Edition[]
  loading: boolean
  busy: boolean
  error: string | null
}

function message(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

export const useIngestStore = defineStore('ingest', {
  state: (): IngestState => ({
    batches: [],
    batch: null,
    selectedId: null,
    filter: 'all',
    search: [],
    loading: false,
    busy: false,
    error: null,
  }),

  getters: {
    candidates: (s): Candidate[] => s.batch?.candidates ?? [],

    /** The queue: candidates still awaiting review, in scan order. */
    reviewable(): Candidate[] {
      return this.candidates.filter((c) => REVIEWABLE.includes(c.status))
    },

    /** The reviewable queue narrowed by the active filter chip. */
    visible(): Candidate[] {
      if (this.filter === 'all') return this.reviewable
      return this.reviewable.filter((c) => c.status === this.filter)
    },

    selected(): Candidate | null {
      if (this.selectedId == null) return null
      return this.candidates.find((c) => c.id === this.selectedId) ?? null
    },

    /** Live per-status counts over the reviewable queue, for the header/chips. */
    counts(): Record<CandidateStatus, number> {
      const out = { ready: 0, review: 0, no_match: 0, duplicate: 0, confirmed: 0, skipped: 0 }
      for (const c of this.reviewable) out[c.status]++
      return out
    },

    readyCount(): number {
      return this.counts.ready
    },
  },

  actions: {
    /** Load the list of batches (for the batch picker). */
    async loadBatches(): Promise<void> {
      this.loading = true
      this.error = null
      try {
        this.batches = await listBatches()
      } catch (err) {
        this.error = message(err, 'failed to load batches')
      } finally {
        this.loading = false
      }
    },

    /** Open a batch and select its first reviewable candidate. */
    async openBatch(id: number): Promise<void> {
      this.loading = true
      this.error = null
      try {
        this.batch = await getBatch(id)
        this.selectFirst()
      } catch (err) {
        this.error = message(err, 'failed to open batch')
      } finally {
        this.loading = false
      }
    },

    /** Start a new, empty batch to populate from the desktop (add-by-title / ISBN). */
    async newBatch(): Promise<void> {
      this.busy = true
      this.error = null
      try {
        const b = await createBatch([])
        this.batches = [b, ...this.batches]
        this.batch = b
        this.selectedId = null
      } catch (err) {
        this.error = message(err, 'failed to create batch')
      } finally {
        this.busy = false
      }
    },

    setFilter(f: QueueFilter): void {
      this.filter = f
    },

    select(id: number): void {
      this.selectedId = id
    },

    /** Pick an edition for the selected review candidate, promoting it to ready. */
    async pick(editionId: string): Promise<void> {
      const c = this.selected
      if (!c) return
      await this.run(async () => {
        await pickEdition(c.id, editionId)
        await this.refresh()
      })
    },

    /** Confirm the selected candidate into inventory, then advance to the next. */
    async confirm(series = '', seriesIndex = 0): Promise<void> {
      const c = this.selected
      if (!c) return
      await this.run(async () => {
        await confirmCandidate(c.id, series, seriesIndex)
        await this.refresh()
        this.advanceFrom(c.id)
      })
    },

    /** Skip (drop) the selected candidate, then advance to the next. */
    async skip(): Promise<void> {
      const c = this.selected
      if (!c) return
      await this.run(async () => {
        await skipCandidate(c.id)
        await this.refresh()
        this.advanceFrom(c.id)
      })
    },

    /** Confirm every ready candidate in the batch in one action. */
    async confirmAllReady(): Promise<number> {
      if (!this.batch) return 0
      let confirmed = 0
      await this.run(async () => {
        const res = await confirmReady(this.batch!.id)
        confirmed = res.confirmed
        this.batch = res.batch
        this.selectFirst()
      })
      return confirmed
    },

    /** Add a scanned/typed ISBN to the active batch (manual + add-by-title). */
    async addByIsbn(isbn: string, source: ScanSource = 'title'): Promise<void> {
      if (!this.batch) return
      await this.run(async () => {
        const added = await addCandidate(this.batch!.id, isbn, source)
        await this.refresh()
        this.selectedId = added.id
      })
    },

    /** Run the add-by-title search. */
    async runSearch(query: string): Promise<void> {
      if (!query.trim()) {
        this.search = []
        return
      }
      try {
        this.search = await searchEditions(query)
      } catch (err) {
        this.error = message(err, 'search failed')
      }
    },

    clearSearch(): void {
      this.search = []
    },

    // ---- internals ----

    /** Re-fetch the active batch to reflect a mutation, preserving selection. */
    async refresh(): Promise<void> {
      if (!this.batch) return
      this.batch = await getBatch(this.batch.id)
    },

    /** Wrap a mutating action with the busy flag + error capture. */
    async run(fn: () => Promise<void>): Promise<void> {
      this.busy = true
      this.error = null
      try {
        await fn()
      } catch (err) {
        this.error = message(err, 'action failed')
      } finally {
        this.busy = false
      }
    },

    selectFirst(): void {
      this.selectedId = this.reviewable[0]?.id ?? null
    },

    /** After confirming/skipping fromId, select the next reviewable candidate. */
    advanceFrom(fromId: number): void {
      const queue = this.reviewable
      if (queue.length === 0) {
        this.selectedId = null
        return
      }
      // Prefer the candidate that now occupies fromId's old slot, else the first.
      const next = queue.find((c) => c.id > fromId) ?? queue[0]
      this.selectedId = next?.id ?? null
    },
  },
})
