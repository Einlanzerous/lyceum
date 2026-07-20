// Accounts, sessions and household administration (LYCM-801).

import { readError } from './client'
import { apiFetch, apiJSON, apiSend, suppressUnauthorized } from './http'

/** A person with an account on this server. */
export interface User {
  id: number
  email: string
  display_name: string
  is_owner: boolean
}

/** One signed-in device. Carries no token material — revoke is by row id. */
export interface Device {
  id: number
  device_label: string
  created_at: string
  last_seen_at: string | null
  current: boolean
}

/** A household-list row: the account plus enough to tell active from absent. */
export interface Member extends User {
  /** null ⇒ never signed in on any device. */
  last_seen_at: string | null
  /** non-null ⇒ an unredeemed invite is outstanding, i.e. "invite pending". */
  invite_expires_at: string | null
  session_count: number
}

/** The one-time secret. Returned by the call that mints it, and never again. */
export interface Invite {
  user: User
  invite_token: string
  /** The short, human-typeable code that stands for the same invite (LYCM-88). */
  pairing_code: string
}

/**
 * Redeem an invite for a durable session on this device.
 *
 * The 401 this can throw is *expected* — it is how a wrong, spent, or expired
 * invite presents (the server deliberately cannot tell us which). So it must not
 * trip the app-wide "you've been signed out" reaction; the sign-in screen shows
 * its own message.
 */
export async function redeemInvite(
  token: string,
  deviceLabel: string,
): Promise<{ user: User; session_token: string }> {
  return suppressUnauthorized(async () => {
    const res = await apiFetch('/auth/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      // Invites get pasted out of chat apps and terminal logs, so they arrive
      // wrapped, padded, and newline-ridden. Clean it here rather than making a
      // person hunt for the stray space.
      body: JSON.stringify({ token: token.replace(/\s+/g, ''), device_label: deviceLabel }),
    })
    if (!res.ok) throw await readError(res)
    return (await res.json()) as { user: User; session_token: string }
  })
}

/**
 * Redeem a short pairing code for a session — the type-it-in alternative to a
 * token (LYCM-88). Like redeemInvite, a 401 is expected input (wrong/spent/
 * expired) and must not trip the app-wide sign-out; a 429 means the code path's
 * rate limit was hit and is surfaced to the sign-in screen as its own message.
 */
export async function redeemPairingCode(
  code: string,
  deviceLabel: string,
): Promise<{ user: User; session_token: string }> {
  return suppressUnauthorized(async () => {
    const res = await apiFetch('/auth/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, device_label: deviceLabel }),
    })
    if (!res.ok) throw await readError(res)
    return (await res.json()) as { user: User; session_token: string }
  })
}

/**
 * Raised by ssoCloudflare when Cloudflare Access verified the person at the edge
 * but no Lyceum account carries that email (LYCM-803). It is not a broken
 * session — it is a real, explainable state: the household owner hasn't invited
 * this address yet. The sign-in screen names the email so the person knows
 * exactly what to ask for.
 */
export class SSONoAccountError extends Error {
  email: string
  constructor(email: string) {
    super(`no Lyceum account for ${email}`)
    this.name = 'SSONoAccountError'
    this.email = email
  }
}

/**
 * Trade the Cloudflare Access identity for a Lyceum session (LYCM-803).
 *
 * Behind the Cloudflare edge the tunnel injects a verified `Cf-Access-Jwt-
 * Assertion` header that this endpoint checks; the browser never sees or sets
 * it. Returns the session on success, or null when SSO is unavailable — the
 * feature is off, or the request didn't come through the tunnel (LAN/direct), or
 * the token didn't verify. Those are all silent fall-throughs to manual sign-in.
 * A verified-but-unknown email is the one loud case: it throws SSONoAccountError.
 *
 * A 401 here is expected (disabled/unverified), so it must not trip the app-wide
 * "signed out" reaction — hence suppressUnauthorized, as with redeemInvite.
 */
export async function ssoCloudflare(): Promise<{ user: User; session_token: string } | null> {
  return suppressUnauthorized(async () => {
    const res = await apiFetch('/auth/sso/cloudflare', { method: 'POST' })
    if (res.ok) {
      return (await res.json()) as { user: User; session_token: string }
    }
    if (res.status === 403) {
      const body = (await res.json().catch(() => ({}))) as { email?: string }
      throw new SSONoAccountError(body.email ?? '')
    }
    // 401 sso_disabled / unauthorized (or anything else) → no SSO available here.
    return null
  })
}

/**
 * The signed-in account, or null when the server wants a session and we have
 * none. A 200 with no token means the server has enforcement off and is serving
 * us as the owner — which is exactly the pre-accounts behaviour.
 */
export async function fetchMe(): Promise<User | null> {
  return suppressUnauthorized(async () => {
    const res = await apiFetch('/auth/me')
    if (res.status === 401) return null
    if (!res.ok) throw await readError(res)
    return (await res.json()) as User
  })
}

/** Rename yourself. The name lives on the account and follows you across devices. */
export function updateDisplayName(displayName: string): Promise<User> {
  return apiSend<User>('PATCH', '/auth/me', { display_name: displayName })
}

/** Sign out — this device only. */
export async function signOut(): Promise<void> {
  await apiFetch('/auth/session', { method: 'DELETE' })
}

/** Your signed-in devices, most recently used first. */
export function listDevices(): Promise<Device[]> {
  return apiJSON<Device[]>('/auth/sessions')
}

/** Sign one of your own devices out. */
export async function revokeDevice(id: number): Promise<void> {
  await apiSend<void>('DELETE', `/auth/sessions/${id}`)
}

// --- Household (owner only) ---

/**
 * Raised when the server has accounts but administration switched off
 * (LYCEUM_AUTH=false → 403). It is not a permissions failure to apologise for —
 * it is a server that cannot tell who is asking, so it refuses to mint
 * credentials. The Household view renders it as its own explained state.
 */
export class AdminDisabledError extends Error {
  constructor() {
    super('household administration is switched off on this server')
    this.name = 'AdminDisabledError'
  }
}

async function adminJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await apiFetch(path, init)
  if (res.status === 403) throw new AdminDisabledError()
  if (!res.ok) throw await readError(res)
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

/** Everyone on this server, owner first. */
export function listMembers(): Promise<Member[]> {
  return adminJSON<Member[]>('/admin/users')
}

/** Add a housemate. Returns their one-time invite — shown once, never again. */
export function inviteMember(email: string, displayName: string): Promise<Invite> {
  return adminJSON<Invite>('/admin/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, display_name: displayName }),
  })
}

/** A fresh invite for someone who already has an account (2nd device, lost key). */
export function reinviteMember(id: number): Promise<Invite> {
  return adminJSON<Invite>(`/admin/users/${id}/invite`, { method: 'POST' })
}

/** Remove a housemate. Deletes their reading positions; the shelf is untouched. */
export async function removeMember(id: number): Promise<void> {
  await adminJSON<void>(`/admin/users/${id}`, { method: 'DELETE' })
}
