import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, RouterLinkStub } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import IngestVerifyView from './IngestVerifyView.vue'
import { useIngestStore } from '@/stores/ingest'
import type { Batch, Candidate, CandidateStatus } from '@/api/ingest'

// Real store actions run against a mocked API layer so we exercise the view's
// per-book series drafts, confirm feedback, and modal logic end-to-end
// (LYCM-95, LYCM-75).
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
  localStorage.clear()
})

describe('IngestVerifyView', () => {
  it('does not carry a confirmed book’s series onto the next book', async () => {
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

    // Assign a series to the first book, then confirm it.
    const inputs = wrapper.findAll('.detail__series-inputs input')
    await inputs[0].setValue('The Expanse')
    await inputs[1].setValue('1')
    await wrapper.find('.detail__actions .btn--brass').trigger('click')
    await flushPromises()

    // Confirm sends exactly what was entered...
    expect(api.confirmCandidate).toHaveBeenCalledWith(1, 'The Expanse', 1)
    // ...and the next book starts blank — no inherited series, no auto-increment.
    expect(store.selected?.id).toBe(2)
    const next = wrapper.findAll('.detail__series-inputs input')
    expect((next[0].element as HTMLInputElement).value).toBe('')
    expect((next[1].element as HTMLInputElement).value).toBe('')
  })

  it('keeps each book’s series draft when you switch between books', async () => {
    vi.mocked(api.getBatch).mockResolvedValue(mkBatch([cand(1, 'ready'), cand(2, 'ready')]))

    const wrapper = mountView()
    const store = useIngestStore()
    await store.openBatch(1)
    await flushPromises()

    // Draft a series on book 1.
    let inputs = wrapper.findAll('.detail__series-inputs input')
    await inputs[0].setValue('The Expanse')
    await inputs[1].setValue('1')

    // Move to book 2 — its fields are its own (blank) — and draft something else.
    store.select(2)
    await flushPromises()
    inputs = wrapper.findAll('.detail__series-inputs input')
    expect((inputs[0].element as HTMLInputElement).value).toBe('')
    await inputs[0].setValue('Foundation')

    // Back to book 1: its draft is restored, not lost to the switch.
    store.select(1)
    await flushPromises()
    inputs = wrapper.findAll('.detail__series-inputs input')
    expect((inputs[0].element as HTMLInputElement).value).toBe('The Expanse')
    expect((inputs[1].element as HTMLInputElement).value).toBe('1')

    // And book 2 kept its own.
    store.select(2)
    await flushPromises()
    inputs = wrapper.findAll('.detail__series-inputs input')
    expect((inputs[0].element as HTMLInputElement).value).toBe('Foundation')
  })

  it('shows a confirm notice and a batch-complete banner when nothing is left', async () => {
    vi.mocked(api.getBatch)
      .mockResolvedValueOnce(mkBatch([cand(1, 'ready')]))
      .mockResolvedValueOnce(mkBatch([cand(1, 'confirmed')]))
    vi.mocked(api.confirmCandidate).mockResolvedValue({
      candidate: cand(1, 'confirmed'),
      inventory: { id: 1, isbn: '9780000000001', state: 'wanted' },
    })

    const wrapper = mountView()
    const store = useIngestStore()
    await store.openBatch(1)
    await flushPromises()

    await wrapper.find('.detail__actions .btn--brass').trigger('click')
    await flushPromises()

    expect(store.confirmedCount).toBe(1)
    const done = wrapper.find('.ing__done')
    expect(done.exists()).toBe(true)
    expect(done.text()).toContain('Batch complete')
    expect(done.text()).toContain('1 book confirmed')
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
