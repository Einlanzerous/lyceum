// The single place an API request leaves the app (LYCM-801).
//
// Before accounts, every module called bare `fetch(apiUrl(...))` — 18 call sites
// with nowhere to attach a credential. Everything now goes through apiFetch, so
// the session rides along and a 401 is noticed exactly once, in one place.
//
// Two credentials, because one is not enough:
//
//   - `Authorization: Bearer` — what the native shells send. The Wails build
//     calls a *different origin*, where a cookie would need CORS credentials and
//     a non-wildcard allowlist; a header sidesteps that.
//   - the `lyceum_session` cookie the server sets at sign-in — what the browser
//     sends on its own. This is not belt-and-braces: the shelf renders covers as
//     plain `<img src="/books/{id}/cover">`, and an <img> request cannot carry a
//     header. Without the cookie every cover would 401 the moment the server
//     turns enforcement on.
//
// `credentials: 'include'` is what makes the cookie travel on the cross-origin
// native path too; same-origin web sends it regardless.

import { apiUrl } from './base'
import { readError } from './client'

const TOKEN_KEY = 'lyceum.session_token'

// undefined = not yet read from storage; null = read, none held.
let tokenCache: string | null | undefined

/** The session token this device holds, or '' when signed out. */
export function getSessionToken(): string {
  if (tokenCache === undefined) {
    try {
      tokenCache = localStorage.getItem(TOKEN_KEY)
    } catch {
      tokenCache = null
    }
  }
  return tokenCache ?? ''
}

/** Persist (or, with '', clear) the session token this device carries. */
export function setSessionToken(token: string): void {
  tokenCache = token || null
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    // localStorage blocked (private mode): keep it in memory so this session
    // still works, it just won't survive a reload.
  }
}

/**
 * Why a request came back 401.
 *
 * `removed` is not something the server tells us — a removed account and an
 * expired session both just stop resolving. We infer it: if the token we were
 * holding stops working, the account may be gone. The copy is deliberately
 * written to be true either way; see SessionEndedDialog.
 */
export type SessionEndReason = 'expired' | 'removed'

type UnauthorizedHandler = (reason: SessionEndReason) => void

let onUnauthorized: UnauthorizedHandler | null = null

/**
 * Register the app-wide reaction to a 401. The auth store installs this on boot;
 * keeping it a callback rather than importing the store here avoids a cycle
 * (store → api → store).
 */
export function setUnauthorizedHandler(fn: UnauthorizedHandler | null): void {
  onUnauthorized = fn
}

// A counter, not a flag: two suppressed calls can overlap (the boot /auth/me and
// a sign-in), and with a bare boolean whichever settled first would re-arm the
// global handler while the other was still in flight.
let suppressDepth = 0

/** Suppress the global 401 reaction — the sign-in screen handles its own. */
export function suppressUnauthorized<T>(fn: () => Promise<T>): Promise<T> {
  suppressDepth++
  return fn().finally(() => {
    suppressDepth--
  })
}

/**
 * Fetch an API path with this device's session attached.
 *
 * A 401 fires the app-wide handler (which surfaces the "you've been signed out"
 * sheet) *and* still rejects, so the calling code's own error path runs normally
 * — the caller never has to know about sessions.
 */
export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers)
  const token = getSessionToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(apiUrl(path), { ...init, headers, credentials: 'include' })

  if (res.status === 401 && suppressDepth === 0) {
    // Only a token we *held* can expire. A 401 with no token at all just means
    // the server enforces auth and we were never signed in — the router sends us
    // to the front door rather than claiming a session ended.
    onUnauthorized?.(token ? 'expired' : 'removed')
  }
  return res
}

/** apiFetch + throw on any non-2xx, for the many callers that want just that. */
export async function apiFetchOk(path: string, init: RequestInit = {}): Promise<Response> {
  const res = await apiFetch(path, init)
  if (!res.ok) throw await readError(res)
  return res
}

/**
 * apiFetchOk + decode JSON.
 *
 * 204 is not a JSON document. The delete/revoke routes answer No Content, and
 * calling res.json() on an empty body throws — so a successful revoke would blow
 * up in the caller and never refresh the list.
 */
export async function apiJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await apiFetchOk(path, init)
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** JSON-body request helper (POST/PATCH/DELETE with a body). */
export function apiSend<T>(method: string, path: string, body?: unknown): Promise<T> {
  return apiJSON<T>(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

/** Test seam: drop the cached token so the next read hits storage again. */
export function __resetSessionCache(): void {
  tokenCache = undefined
}
