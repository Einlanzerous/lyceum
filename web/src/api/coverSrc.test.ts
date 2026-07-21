import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// coverSrc reaches for these three seams; mock them so we can drive the native
// path deterministically without a real backend or browser.
vi.mock('./base', () => ({ isNativeShell: vi.fn() }))
vi.mock('./client', () => ({ coverUrl: vi.fn((id: number) => `/books/${id}/cover`) }))
vi.mock('./http', () => ({ apiFetch: vi.fn() }))

import { isNativeShell } from './base'
import { coverUrl } from './client'
import { apiFetch } from './http'
import { coverSrc, MAX_CONCURRENT_COVER_FETCHES } from './coverSrc'

const isNative = vi.mocked(isNativeShell)
const fetch = vi.mocked(apiFetch)

const flush = (): Promise<void> => new Promise((r) => setTimeout(r, 0))

// Each apiFetch call parks on a deferred we resolve by hand, so the number of
// unresolved deferreds == the number of fetches currently in flight.
let deferreds: Array<(res: unknown) => void>
function okResponse(): unknown {
  return { ok: true, blob: () => Promise.resolve(new Blob()) }
}

beforeEach(() => {
  deferreds = []
  fetch.mockReset()
  fetch.mockImplementation(() => new Promise((resolve) => deferreds.push(resolve)) as never)
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:mock', revokeObjectURL: vi.fn() })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('coverSrc', () => {
  it('returns the plain same-origin URL on the web build and never fetches bytes', () => {
    isNative.mockReturnValue(false)
    expect(coverSrc(1)).toBe('/books/1/cover')
    expect(vi.mocked(coverUrl)).toHaveBeenCalledWith(1)
    expect(fetch).not.toHaveBeenCalled()
  })

  it('caps concurrent cover fetches and drains the queue as slots free (native)', async () => {
    isNative.mockReturnValue(true)
    const ids = Array.from({ length: MAX_CONCURRENT_COVER_FETCHES + 3 }, (_, i) => 1000 + i)

    // First render kicks every card's fetch, but only the cap runs at once.
    for (const id of ids) expect(coverSrc(id)).toBe('')
    expect(fetch).toHaveBeenCalledTimes(MAX_CONCURRENT_COVER_FETCHES)

    // Landing one cover frees exactly one slot for the next queued fetch.
    deferreds.shift()!(okResponse())
    await flush()
    expect(fetch).toHaveBeenCalledTimes(MAX_CONCURRENT_COVER_FETCHES + 1)

    // Drain the rest; every requested cover is eventually fetched exactly once.
    while (deferreds.length) {
      deferreds.shift()!(okResponse())
      await flush()
    }
    expect(fetch).toHaveBeenCalledTimes(ids.length)
  })

  it('fetches a given cover only once even if requested repeatedly (native)', () => {
    isNative.mockReturnValue(true)
    coverSrc(7777)
    coverSrc(7777)
    coverSrc(7777)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
