import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useIngestStore } from './ingest'
import type { Batch, Candidate, CandidateStatus } from '@/api/ingest'

vi.mock('@/api/ingest', () => ({
  listBatches: vi.fn(),
  getBatch: vi.fn(),
  createBatch: vi.fn(),
  addCandidate: vi.fn(),
  pickEdition: vi.fn(),
  confirmCandidate: vi.fn(),
  skipCandidate: vi.fn(),
  confirmReady: vi.fn(),
  searchEditions: vi.fn(),
}))

import * as api from '@/api/ingest'

const cand = (id: number, status: CandidateStatus): Candidate => ({
  id,
  batch_id: 1,
  isbn: `978000000000${id}`,
  source: 'camera',
  status,
  confidence: status === 'ready' ? 0.95 : status === 'review' ? 0.6 : 0,
  editions:
    status === 'review'
      ? [
          { id: 'a', title: 'A' },
          { id: 'b', title: 'B' },
        ]
      : [],
  title: status === 'no_match' ? '' : `Book ${id}`,
})

const mkBatch = (cands: Candidate[]): Batch => ({
  id: 1,
  status: 'open',
  created_at: '2026-07-10T12:00:00Z',
  counts: {},
  candidates: cands,
})

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('ingest store', () => {
  it('openBatch loads candidates, selects the first reviewable, and counts by status', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(
      mkBatch([cand(1, 'ready'), cand(2, 'review'), cand(3, 'no_match'), cand(4, 'confirmed')]),
    )
    const store = useIngestStore()
    await store.openBatch(1)

    // confirmed/skipped drop out of the review queue.
    expect(store.reviewable.map((c) => c.id)).toEqual([1, 2, 3])
    expect(store.selected?.id).toBe(1)
    expect(store.counts.ready).toBe(1)
    expect(store.counts.review).toBe(1)
    expect(store.counts.no_match).toBe(1)
  })

  it('exposes confirmed and outstanding counts', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(
      mkBatch([cand(1, 'ready'), cand(2, 'review'), cand(3, 'no_match'), cand(4, 'confirmed')]),
    )
    const store = useIngestStore()
    await store.openBatch(1)
    expect(store.confirmedCount).toBe(1) // candidate 4
    expect(store.outstanding).toBe(2) // ready + review, not no_match
  })

  it('syncs the picker list when a batch auto-closes after its last confirm', async () => {
    vi.mocked(api.listBatches).mockResolvedValue([mkBatch([])])
    const store = useIngestStore()
    await store.loadBatches()
    expect(store.batches[0].status).toBe('open')

    // Confirming the last ready candidate closes the batch server-side; the
    // refreshed batch reports 'confirmed', which must reach the picker list too.
    vi.mocked(api.getBatch)
      .mockResolvedValueOnce(mkBatch([cand(1, 'ready')]))
      .mockResolvedValueOnce({ ...mkBatch([cand(1, 'confirmed')]), status: 'confirmed' })
    vi.mocked(api.confirmCandidate).mockResolvedValue({
      candidate: cand(1, 'confirmed'),
      inventory: { id: 1, isbn: '9780000000001', state: 'wanted' },
    })
    await store.openBatch(1)
    await store.confirm('', 0)

    expect(store.batch?.status).toBe('confirmed')
    expect(store.batches[0].status).toBe('confirmed') // no longer stale-open
  })

  it('setFilter narrows the visible queue', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(mkBatch([cand(1, 'ready'), cand(2, 'review')]))
    const store = useIngestStore()
    await store.openBatch(1)

    store.setFilter('review')
    expect(store.visible.map((c) => c.id)).toEqual([2])
    store.setFilter('all')
    expect(store.visible.map((c) => c.id)).toEqual([1, 2])
  })

  it('confirm shelves the selected candidate and advances to the next', async () => {
    const full = mkBatch([cand(1, 'ready'), cand(2, 'review')])
    const afterConfirm = mkBatch([cand(2, 'review')]) // candidate 1 gone from the queue
    vi.mocked(api.getBatch).mockResolvedValueOnce(full).mockResolvedValueOnce(afterConfirm)
    vi.mocked(api.confirmCandidate).mockResolvedValue({
      candidate: { ...cand(1, 'confirmed') },
      inventory: { id: 1, isbn: '9780000000001', state: 'wanted' },
    })

    const store = useIngestStore()
    await store.openBatch(1)
    expect(store.selected?.id).toBe(1)

    await store.confirm('The Cycle', 2)

    expect(api.confirmCandidate).toHaveBeenCalledWith(1, 'The Cycle', 2)
    expect(store.selected?.id).toBe(2) // advanced to the next reviewable
  })

  it('confirmAllReady replaces the batch with the server result', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(mkBatch([cand(1, 'ready'), cand(2, 'review')]))
    vi.mocked(api.confirmReady).mockResolvedValue({
      confirmed: 1,
      batch: mkBatch([cand(2, 'review')]),
    })
    const store = useIngestStore()
    await store.openBatch(1)

    const n = await store.confirmAllReady()
    expect(n).toBe(1)
    expect(store.reviewable.map((c) => c.id)).toEqual([2])
  })

  it('reResolve adds the corrected ISBN, drops the no-match row, and selects the replacement', async () => {
    const full = mkBatch([cand(1, 'no_match'), cand(2, 'ready')])
    const afterResolve = mkBatch([cand(2, 'ready'), cand(3, 'ready')]) // 1 skipped, 3 added
    vi.mocked(api.getBatch).mockResolvedValueOnce(full).mockResolvedValueOnce(afterResolve)
    vi.mocked(api.addCandidate).mockResolvedValue(cand(3, 'ready'))
    vi.mocked(api.skipCandidate).mockResolvedValue()

    const store = useIngestStore()
    await store.openBatch(1)
    expect(store.selected?.id).toBe(1)

    await store.reResolve(1, ' 9780306406157 ')

    expect(api.addCandidate).toHaveBeenCalledWith(1, '9780306406157', 'manual')
    expect(api.skipCandidate).toHaveBeenCalledWith(1)
    expect(store.selected?.id).toBe(3) // the freshly re-resolved candidate
  })

  it('reResolve ignores a blank ISBN', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(mkBatch([cand(1, 'no_match')]))
    const store = useIngestStore()
    await store.openBatch(1)

    await store.reResolve(1, '   ')
    expect(api.addCandidate).not.toHaveBeenCalled()
    expect(api.skipCandidate).not.toHaveBeenCalled()
  })

  it('records an error when a batch fails to load', async () => {
    vi.mocked(api.getBatch).mockRejectedValue(new Error('boom'))
    const store = useIngestStore()
    await store.openBatch(1)
    expect(store.error).toBe('boom')
    expect(store.batch).toBeNull()
  })
})
