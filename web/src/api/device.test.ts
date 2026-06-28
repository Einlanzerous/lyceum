import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { getDeviceId, resetDeviceIdCache } from './device'

describe('getDeviceId', () => {
  beforeEach(() => {
    localStorage.clear()
    resetDeviceIdCache()
  })
  afterEach(() => {
    localStorage.clear()
    resetDeviceIdCache()
  })

  it('generates and persists an id on first use', () => {
    const id = getDeviceId()
    expect(id).toBeTruthy()
    expect(localStorage.getItem('lyceum.device_id')).toBe(id)
  })

  it('returns the same id across calls', () => {
    expect(getDeviceId()).toBe(getDeviceId())
  })

  it('reuses a persisted id after a cache reset (simulated reload)', () => {
    const first = getDeviceId()
    resetDeviceIdCache()
    expect(getDeviceId()).toBe(first)
  })

  it('still produces a valid UUID in an insecure context (no crypto.randomUUID)', () => {
    // crypto.randomUUID is undefined on plain-HTTP LAN origins; getRandomValues
    // remains available. The id must still be a well-formed v4 UUID.
    const original = crypto.randomUUID
    try {
      // @ts-expect-error simulate the insecure-context absence
      crypto.randomUUID = undefined
      const id = getDeviceId()
      expect(id).toMatch(
        /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      )
      expect(localStorage.getItem('lyceum.device_id')).toBe(id)
    } finally {
      crypto.randomUUID = original
    }
  })
})
