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
})
