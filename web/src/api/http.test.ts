import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  __resetSessionCache,
  apiFetch,
  getSessionToken,
  setSessionToken,
  setUnauthorizedHandler,
  suppressUnauthorized,
} from './http'

function mockFetch(impl: (url: string, init?: RequestInit) => Response) {
  const fn = vi.fn((url: string, init?: RequestInit) => Promise.resolve(impl(url, init)))
  vi.stubGlobal('fetch', fn)
  return fn
}

beforeEach(() => {
  localStorage.clear()
  __resetSessionCache()
  setUnauthorizedHandler(null)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('session token', () => {
  it('round-trips through storage', () => {
    expect(getSessionToken()).toBe('')
    setSessionToken('lyc_abc')
    expect(getSessionToken()).toBe('lyc_abc')
    setSessionToken('')
    expect(getSessionToken()).toBe('')
    expect(localStorage.getItem('lyceum.session_token')).toBeNull()
  })
})

describe('apiFetch', () => {
  it('sends the bearer token when this device holds one', async () => {
    setSessionToken('lyc_abc')
    const fn = mockFetch(() => new Response('{}', { status: 200 }))

    await apiFetch('/library')

    const init = fn.mock.calls[0]![1]!
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer lyc_abc')
  })

  it('always includes credentials, so the cookie rides along', async () => {
    // Load-bearing: the shelf renders covers as <img src>, which cannot carry a
    // header. Without the cookie every cover 401s once the server enforces auth.
    const fn = mockFetch(() => new Response('{}', { status: 200 }))

    await apiFetch('/library')

    expect(fn.mock.calls[0]![1]!.credentials).toBe('include')
  })

  it('omits Authorization entirely when signed out', async () => {
    const fn = mockFetch(() => new Response('{}', { status: 200 }))

    await apiFetch('/library')

    expect(new Headers(fn.mock.calls[0]![1]!.headers).has('Authorization')).toBe(false)
  })

  it('reports a 401 as "expired" when a token we held stopped working', async () => {
    setSessionToken('lyc_stale')
    mockFetch(() => new Response('nope', { status: 401 }))
    const onEnd = vi.fn()
    setUnauthorizedHandler(onEnd)

    await apiFetch('/library')

    expect(onEnd).toHaveBeenCalledWith('expired')
  })

  it('does not claim a session "expired" when we never had one', async () => {
    mockFetch(() => new Response('nope', { status: 401 }))
    const onEnd = vi.fn()
    setUnauthorizedHandler(onEnd)

    await apiFetch('/library')

    expect(onEnd).toHaveBeenCalledWith('removed')
  })

  it('stays quiet during sign-in, where a 401 is the expected answer', async () => {
    // A wrong/spent/expired invite is a 401. Firing the app-wide "you've been
    // signed out" sheet at someone who is trying to sign *in* would be absurd.
    mockFetch(() => new Response('bad invite', { status: 401 }))
    const onEnd = vi.fn()
    setUnauthorizedHandler(onEnd)

    await suppressUnauthorized(() => apiFetch('/auth/session', { method: 'POST' }))

    expect(onEnd).not.toHaveBeenCalled()
  })

  it('still resolves the response on 401, so callers handle their own errors', async () => {
    mockFetch(() => new Response('nope', { status: 401 }))
    const res = await apiFetch('/library')
    expect(res.status).toBe(401)
  })
})

describe('204 responses', () => {
  it('does not try to parse No Content as JSON', async () => {
    // DELETE /auth/sessions/{id} answers 204. Parsing an empty body throws, which
    // would blow up the Settings "Revoke" button after the revoke had already
    // succeeded server-side.
    mockFetch(() => new Response(null, { status: 204 }))
    const { apiSend } = await import('./http')

    await expect(apiSend('DELETE', '/auth/sessions/3')).resolves.toBeUndefined()
  })
})

describe('suppression nests', () => {
  it('keeps suppressing while an outer suppressed call is still in flight', async () => {
    mockFetch(() => new Response('nope', { status: 401 }))
    const onEnd = vi.fn()
    setUnauthorizedHandler(onEnd)

    await suppressUnauthorized(async () => {
      // An inner suppressed call settles first; with a bare boolean it would
      // re-arm the handler and the outer 401 would fire the signed-out sheet.
      await suppressUnauthorized(() => apiFetch('/auth/me'))
      await apiFetch('/auth/session', { method: 'POST' })
    })

    expect(onEnd).not.toHaveBeenCalled()
  })
})
