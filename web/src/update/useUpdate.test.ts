import { afterEach, describe, expect, it, vi } from 'vitest'
import { __resetServerCache, __setNativeShell } from '@/api/base'
import {
  __resetUpdate,
  __setCurrentVersion,
  checkForUpdate,
  isNewerVersion,
  useUpdate,
} from './useUpdate'

// A fetch stub returning a GitHub "latest release" payload.
function releaseFetch(tag: string, url = 'https://example/rel') {
  return vi.fn(async () =>
    new Response(JSON.stringify({ tag_name: tag, html_url: url }), { status: 200 }),
  ) as unknown as typeof fetch
}

afterEach(() => {
  __setNativeShell(null)
  __setCurrentVersion(null)
  __resetUpdate()
  __resetServerCache()
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('isNewerVersion', () => {
  it('compares semver by major, minor, then patch', () => {
    expect(isNewerVersion('v0.2.0', '0.1.0')).toBe(true)
    expect(isNewerVersion('1.0.0', '0.9.9')).toBe(true)
    expect(isNewerVersion('0.1.1', '0.1.0')).toBe(true)
    expect(isNewerVersion('0.1.0', '0.1.0')).toBe(false)
    expect(isNewerVersion('0.1.0', '0.2.0')).toBe(false)
  })

  it('tolerates a leading v and a pre-release suffix, and rejects junk', () => {
    expect(isNewerVersion('v1.2.3-rc1', 'v1.2.2')).toBe(true)
    expect(isNewerVersion('not-a-version', '0.1.0')).toBe(false)
    expect(isNewerVersion('0.2.0', '')).toBe(false)
  })
})

describe('checkForUpdate (native shell)', () => {
  it('surfaces a newer release', async () => {
    __setNativeShell(true)
    __setCurrentVersion('0.1.0')
    await checkForUpdate(releaseFetch('v0.2.0', 'https://gh/rel/0.2.0'))
    expect(useUpdate().update.value).toEqual({ version: '0.2.0', url: 'https://gh/rel/0.2.0' })
  })

  it('ignores a release that is not newer', async () => {
    __setNativeShell(true)
    __setCurrentVersion('0.2.0')
    await checkForUpdate(releaseFetch('v0.2.0'))
    expect(useUpdate().update.value).toBeNull()
  })

  it('stays silent for a dismissed version, then reappears for a newer one', async () => {
    __setNativeShell(true)
    __setCurrentVersion('0.1.0')

    await checkForUpdate(releaseFetch('v0.2.0'))
    useUpdate().dismiss()
    expect(useUpdate().update.value).toBeNull()

    // Same version → still dismissed.
    await checkForUpdate(releaseFetch('v0.2.0'))
    expect(useUpdate().update.value).toBeNull()

    // A newer version → shows again.
    await checkForUpdate(releaseFetch('v0.3.0'))
    expect(useUpdate().update.value?.version).toBe('0.3.0')
  })

  it('swallows a failed fetch (no banner, no throw)', async () => {
    __setNativeShell(true)
    __setCurrentVersion('0.1.0')
    const boom = vi.fn(async () => new Response('nope', { status: 500 })) as unknown as typeof fetch
    await expect(checkForUpdate(boom)).resolves.toBeUndefined()
    expect(useUpdate().update.value).toBeNull()
  })
})

describe('checkForUpdate (disabled cases)', () => {
  it('is a no-op in the web build', async () => {
    __setNativeShell(false)
    __setCurrentVersion('0.1.0')
    const f = releaseFetch('v9.9.9')
    await checkForUpdate(f)
    expect(f).not.toHaveBeenCalled()
    expect(useUpdate().update.value).toBeNull()
  })

  it('is a no-op for a dev/unversioned build (no baked version)', async () => {
    __setNativeShell(true)
    __setCurrentVersion('')
    const f = releaseFetch('v9.9.9')
    await checkForUpdate(f)
    expect(f).not.toHaveBeenCalled()
    expect(useUpdate().update.value).toBeNull()
  })
})
