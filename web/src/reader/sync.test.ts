import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPositionSync } from './sync'
import type { PositionInput } from '@/api/types'

const info = (cfi: string, progress = 0.3) => ({ cfi, progress })

beforeEach(() => {
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('createPositionSync.schedule', () => {
  it('debounces rapid page turns into one PUT with the latest position', async () => {
    const put = vi.fn<[PositionInput], Promise<void>>().mockResolvedValue()
    const sync = createPositionSync(7, { put, deviceId: 'dev-1', debounceMs: 1000 })

    sync.schedule(info('cfi-a'))
    sync.schedule(info('cfi-b'))
    sync.schedule(info('cfi-c', 0.5))
    expect(put).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1000)
    expect(put).toHaveBeenCalledOnce()
    const sent = put.mock.calls[0]![0]
    expect(sent).toMatchObject({
      book_id: 7,
      device_id: 'dev-1',
      cfi: 'cfi-c',
      progress: 0.5,
    })
    expect(typeof sent.updated_at).toBe('string')
  })

  it('does not send until the quiet period elapses', async () => {
    const put = vi.fn().mockResolvedValue(undefined)
    const sync = createPositionSync(1, { put, deviceId: 'd', debounceMs: 1000 })
    sync.schedule(info('cfi-a'))
    await vi.advanceTimersByTimeAsync(900)
    expect(put).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(100)
    expect(put).toHaveBeenCalledOnce()
  })
})

describe('createPositionSync.flush', () => {
  it('sends the pending position immediately and cancels the timer', async () => {
    const put = vi.fn().mockResolvedValue(undefined)
    const sync = createPositionSync(1, { put, deviceId: 'd', debounceMs: 1000 })
    sync.schedule(info('cfi-x'))
    await sync.flush()
    expect(put).toHaveBeenCalledOnce()
    // The original debounce timer must not fire a second send.
    await vi.advanceTimersByTimeAsync(2000)
    expect(put).toHaveBeenCalledOnce()
  })

  it('is a no-op when nothing is pending', async () => {
    const put = vi.fn().mockResolvedValue(undefined)
    const sync = createPositionSync(1, { put })
    await sync.flush()
    expect(put).not.toHaveBeenCalled()
  })
})

describe('createPositionSync.flushOnUnload', () => {
  it('uses the keepalive sender when provided', () => {
    const put = vi.fn().mockResolvedValue(undefined)
    const putKeepalive = vi.fn()
    const sync = createPositionSync(3, { put, putKeepalive, deviceId: 'd' })
    sync.flushOnUnload(info('cfi-final', 0.9))
    expect(putKeepalive).toHaveBeenCalledOnce()
    expect(putKeepalive.mock.calls[0]![0]).toMatchObject({ book_id: 3, cfi: 'cfi-final' })
    expect(put).not.toHaveBeenCalled()
  })

  it('falls back to the normal sender when no keepalive sender is given', () => {
    const put = vi.fn().mockResolvedValue(undefined)
    const sync = createPositionSync(3, { put, deviceId: 'd' })
    sync.flushOnUnload(info('cfi-final'))
    expect(put).toHaveBeenCalledOnce()
  })
})

describe('createPositionSync.restore', () => {
  it('returns the saved CFI', async () => {
    const get = vi.fn().mockResolvedValue({
      book_id: 1,
      device_id: 'd',
      cfi: 'saved-cfi',
      progress: 0.2,
      updated_at: '2026-06-27T00:00:00.000Z',
    })
    const sync = createPositionSync(1, { get, deviceId: 'd' })
    await expect(sync.restore()).resolves.toBe('saved-cfi')
    expect(get).toHaveBeenCalledWith(1, 'd')
  })

  it('returns null when there is no saved position (404 -> null)', async () => {
    const get = vi.fn().mockResolvedValue(null)
    const sync = createPositionSync(1, { get, deviceId: 'd' })
    await expect(sync.restore()).resolves.toBeNull()
  })

  it('swallows errors and resolves null so the reader still opens', async () => {
    const get = vi.fn().mockRejectedValue(new Error('boom'))
    const onError = vi.fn()
    const sync = createPositionSync(1, { get, onError, deviceId: 'd' })
    await expect(sync.restore()).resolves.toBeNull()
    expect(onError).toHaveBeenCalledOnce()
  })
})
