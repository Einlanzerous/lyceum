// Bridges epub.js relocations to the /sync backend contract. Page turns are
// debounced into a single PUT so rapid flipping doesn't spam the server, and
// the latest position can be flushed immediately on unmount/unload (LYCM-206).
//
// Restore sends THIS device's id; the backend falls back to the latest position
// across all devices when this device has none, so a fresh device resumes where
// another left off — cross-device resume needs no extra client work.

import { getPosition, putPosition } from '@/api/client'
import { getDeviceId } from '@/api/device'
import type { PositionInput } from '@/api/types'
import type { RelocateInfo } from './useReader'

export interface PositionSyncOptions {
  /** Quiet period before a scheduled position is sent. Default 1000ms. */
  debounceMs?: number
  deviceId?: string
  /** Injectable for tests; defaults to the real client. */
  put?: (pos: PositionInput) => Promise<unknown>
  get?: typeof getPosition
  /** Keepalive sender for unload flushes (LYCM-206); falls back to put. */
  putKeepalive?: (pos: PositionInput) => void
  onError?: (err: unknown) => void
}

export interface PositionSync {
  /** Resolve the CFI to open at, or null to start from the beginning. */
  restore(): Promise<string | null>
  /** Debounce-save a new position from a relocate. */
  schedule(info: RelocateInfo): void
  /** Send any pending position now (awaitable). */
  flush(): Promise<void>
  /** Fire-and-forget keepalive send of the given position, for page unload. */
  flushOnUnload(info: RelocateInfo): void
  /** Cancel any pending timer without sending. */
  dispose(): void
}

export function createPositionSync(
  bookId: number,
  options: PositionSyncOptions = {},
): PositionSync {
  const debounceMs = options.debounceMs ?? 1000
  const put = options.put ?? putPosition
  const get = options.get ?? getPosition
  const deviceId = options.deviceId ?? getDeviceId()
  const onError = options.onError ?? (() => {})

  let timer: ReturnType<typeof setTimeout> | undefined
  let pending: RelocateInfo | null = null

  function payload(info: RelocateInfo): PositionInput {
    return {
      book_id: bookId,
      device_id: deviceId,
      cfi: info.cfi,
      progress: info.progress,
      updated_at: new Date().toISOString(),
    }
  }

  async function send(info: RelocateInfo): Promise<void> {
    try {
      await put(payload(info))
    } catch (err) {
      onError(err)
    }
  }

  function clear(): void {
    if (timer !== undefined) {
      clearTimeout(timer)
      timer = undefined
    }
  }

  return {
    async restore(): Promise<string | null> {
      try {
        const pos = await get(bookId, deviceId)
        return pos?.cfi ?? null
      } catch (err) {
        onError(err)
        return null
      }
    },

    schedule(info: RelocateInfo): void {
      pending = info
      clear()
      timer = setTimeout(() => {
        timer = undefined
        const next = pending
        pending = null
        if (next) void send(next)
      }, debounceMs)
    },

    async flush(): Promise<void> {
      clear()
      const next = pending
      pending = null
      if (next) await send(next)
    },

    flushOnUnload(info: RelocateInfo): void {
      clear()
      pending = null
      if (options.putKeepalive) options.putKeepalive(payload(info))
      else void send(info)
    },

    dispose(): void {
      clear()
      pending = null
    },
  }
}
