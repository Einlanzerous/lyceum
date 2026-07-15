import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import IngestVerifyView from './IngestVerifyView.vue'
import { useIngestStore } from '@/stores/ingest'
import type { Batch, Candidate, CandidateStatus } from '@/api/ingest'

// Real store actions run against a mocked API layer so we exercise the view's
// sticky-series / modal logic end-to-end (LYCM-75).
vi.mock('@/api/ingest', () => ({
  listBatches: vi.fn().mockResolvedValue([]),
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
  confidence: 0.95,
  editions: [],
  title: `Book ${id}`,
})

const mkBatch = (cands: Candidate[]): Batch => ({
  id: 1,
  status: 'open',
  created_at: '2026-07-10T12:00:00Z',
  counts: {},
  candidates: cands,
})

function mountView() {
  return mount(IngestVerifyView, {
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn, stubActions: false })],
      stubs: { RouterLink: RouterLinkStub },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.listBatches).mockResolvedValue([])
})

describe('IngestVerifyView', () => {
  it('carries the series onto the next book and auto-increments the index across confirms', async () => {
    vi.mocked(api.getBatch)
      .mockResolvedValueOnce(mkBatch([cand(1, 'ready'), cand(2, 'ready')]))
      .mockResolvedValueOnce(mkBatch([cand(2, 'ready')])) // candidate 1 confirmed away
    vi.mocked(api.confirmCandidate).mockResolvedValue({
      candidate: cand(1, 'confirmed'),
      inventory: { id: 1, isbn: '9780000000001', state: 'wanted' },
    })

    const wrapper = mountView()
    const store = useIngestStore()
    await store.openBatch(1)
    await flushPromises()

    // Assign a series to the first book.
    const inputs = wrapper.findAll('.detail__series-inputs input')
    await inputs[0].setValue('The Expanse')
    await inputs[1].setValue('1')

    await wrapper.find('.detail__actions .btn--brass').trigger('click')
    await flushPromises()

    // The confirm sends the entered index (not the incremented one)...
    expect(api.confirmCandidate).toHaveBeenCalledWith(1, 'The Expanse', 1)
    // ...and the next book inherits the series with the index bumped to 2.
    expect(store.selected?.id).toBe(2)
    const next = wrapper.findAll('.detail__series-inputs input')
    expect((next[0].element as HTMLInputElement).value).toBe('The Expanse')
    expect((next[1].element as HTMLInputElement).value).toBe('2')
  })

  it('opens the add-book modal from the ＋ button and closes it with the back arrow', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(mkBatch([cand(1, 'ready')]))

    const wrapper = mountView()
    const store = useIngestStore()
    await store.openBatch(1)
    await flushPromises()

    expect(wrapper.find('.addm').exists()).toBe(false)

    await wrapper.find('.ing__add-btn').trigger('click')
    // A single combined search field, and back-arrow navigation.
    expect(wrapper.find('.addm').exists()).toBe(true)
    expect(wrapper.findAll('.addm input').length).toBe(1)

    await wrapper.find('.addm__back').trigger('click')
    expect(wrapper.find('.addm').exists()).toBe(false)
  })
})
