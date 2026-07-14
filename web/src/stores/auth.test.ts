import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { __resetSessionCache, getSessionToken, setSessionToken } from '@/api/http'
import { useAuthStore } from './auth'

const OWNER = { id: 1, email: 'ada@home.lan', display_name: 'Ada', is_owner: true }

function mockFetch(impl: (url: string, init?: RequestInit) => Response) {
  const fn = vi.fn((url: string, init?: RequestInit) => Promise.resolve(impl(url, init)))
  vi.stubGlobal('fetch', fn)
  return fn
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

beforeEach(() => {
  localStorage.clear()
  __resetSessionCache()
  setActivePinia(createPinia())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('load', () => {
  it('signs us in when the server answers /auth/me', async () => {
    mockFetch(() => json(200, OWNER))
    const auth = useAuthStore()

    await auth.load()

    expect(auth.status).toBe('signedIn')
    expect(auth.isOwner).toBe(true)
    expect(auth.initial).toBe('A')
  })

  it('treats a 401 as signed out rather than an error', async () => {
    mockFetch(() => json(401, {}))
    const auth = useAuthStore()

    await auth.load()

    expect(auth.status).toBe('signedOut')
    expect(auth.user).toBeNull()
  })

  it('is invisible on a server with enforcement off, which serves us as the owner', async () => {
    // No token, but /auth/me still answers — LYCEUM_AUTH=false. Nobody should be
    // sent to the front door on such a server.
    mockFetch(() => json(200, OWNER))
    const auth = useAuthStore()

    await auth.load()

    expect(auth.status).toBe('signedIn')
    expect(getSessionToken()).toBe('')
  })
})

describe('signIn', () => {
  it('stores the session and adopts the pre-accounts local name', async () => {
    // The upgrade moment: this browser has been reading as "Ada" for months, and
    // the account was seeded by migration with a placeholder. The name must carry
    // over, or "You've been reading as Ada" becomes a lie the moment they sign in.
    localStorage.setItem('lyceum.profileName', 'Ada')
    const placeholder = { ...OWNER, display_name: 'Reader' }

    const fn = mockFetch((url, init) => {
      if (url.endsWith('/auth/session')) return json(200, { user: placeholder, session_token: 'lyc_new' })
      if (url.endsWith('/auth/me') && init?.method === 'PATCH') {
        return json(200, { ...OWNER, display_name: 'Ada' })
      }
      return json(200, OWNER)
    })

    const auth = useAuthStore()
    await auth.signIn('  lyc_invite\n ', 'Chrome on Linux')

    expect(getSessionToken()).toBe('lyc_new')
    expect(auth.displayName).toBe('Ada')

    // Whitespace and newlines are stripped — invites get pasted out of chat apps
    // and terminal logs.
    const body = JSON.parse(fn.mock.calls[0]![1]!.body as string)
    expect(body.token).toBe('lyc_invite')
    expect(body.device_label).toBe('Chrome on Linux')

    // Consumed exactly once: a name the person later changes on the server must
    // not be reverted by a stale local label on the next sign-in.
    expect(localStorage.getItem('lyceum.profileName')).toBeNull()
  })

  it('never overwrites a name the account already carries', async () => {
    localStorage.setItem('lyceum.profileName', 'Stale')
    const fn = mockFetch((url) => {
      if (url.endsWith('/auth/session')) return json(200, { user: OWNER, session_token: 'lyc_new' })
      return json(200, OWNER)
    })

    const auth = useAuthStore()
    await auth.signIn('lyc_invite')

    expect(auth.displayName).toBe('Ada')
    const patched = fn.mock.calls.some((c) => (c[1] as RequestInit | undefined)?.method === 'PATCH')
    expect(patched).toBe(false)
  })
})

describe('signOut', () => {
  it('drops the local token even if the server call fails', async () => {
    // Otherwise a failed revoke leaves a working credential on a device the
    // person just asked to be signed out of — the worst possible outcome.
    setSessionToken('lyc_abc')
    mockFetch(() => {
      throw new Error('network down')
    })

    const auth = useAuthStore()
    await expect(auth.signOut()).rejects.toThrow()

    expect(getSessionToken()).toBe('')
    expect(auth.status).toBe('signedOut')
  })
})

describe('sessionEnded', () => {
  it('clears the credential and records why', () => {
    setSessionToken('lyc_abc')
    const auth = useAuthStore()
    auth.$patch({ status: 'signedIn', user: OWNER })

    auth.sessionEnded('expired')

    expect(getSessionToken()).toBe('')
    expect(auth.status).toBe('signedOut')
    expect(auth.endedReason).toBe('expired')
  })
})
