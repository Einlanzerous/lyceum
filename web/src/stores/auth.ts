import { defineStore } from 'pinia'
import {
  fetchMe,
  redeemInvite,
  signOut as signOutApi,
  updateDisplayName as renameApi,
  type User,
} from '@/api/auth'
import { inferDeviceLabel } from '@/api/device'
import {
  getSessionToken,
  setSessionToken,
  setUnauthorizedHandler,
  type SessionEndReason,
} from '@/api/http'
import { takeLegacyProfileName } from '@/profile'

/**
 * Where the app stands with the server (LYCM-801).
 *
 *  - `unknown`   — haven't asked yet (boot).
 *  - `signedIn`  — /auth/me answered. Note this is *also* the state on a server
 *                  with enforcement off, which serves every request as the owner:
 *                  from the client's side "who am I" has an answer either way, so
 *                  there is nothing special to handle.
 *  - `signedOut` — the server wants a session and we don't have a working one.
 */
export type AuthStatus = 'unknown' | 'signedIn' | 'signedOut'

interface AuthState {
  status: AuthStatus
  user: User | null
  /** Set when a session we *held* stopped working, so we can explain rather than
   *  silently bounce someone mid-chapter to a login screen. */
  endedReason: SessionEndReason | null
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    status: 'unknown',
    user: null,
    endedReason: null,
  }),

  getters: {
    isOwner: (s) => s.user?.is_owner === true,
    /**
     * Whether this device actually signed in — i.e. whether the server enforces
     * auth at all.
     *
     * On a server with LYCEUM_AUTH=false, /auth/me answers with the owner and we
     * never hold a token. "Sign out" and "your devices" are meaningless there:
     * signing out would strand the person on a front door that issues no invites
     * and that they can't get past. So the UI hides both unless we hold a session.
     */
    enforced: () => getSessionToken() !== '',
    displayName: (s) => s.user?.display_name ?? '',
    /** The library avatar's letter. */
    initial(s): string {
      const name = s.user?.display_name?.trim() ?? ''
      return (name[0] ?? 'R').toUpperCase()
    },
  },

  actions: {
    /**
     * Resolve who we are. Called once on boot, before the first render that
     * depends on it.
     */
    async load(): Promise<void> {
      // A 401 from any request now surfaces the "signed out" sheet rather than a
      // generic failure. Installed here (not in the api layer) so the api module
      // doesn't have to import the store back.
      setUnauthorizedHandler((reason) => this.sessionEnded(reason))

      const user = await fetchMe()
      if (user) {
        this.user = user
        this.status = 'signedIn'
        this.endedReason = null
        // Also on a server with enforcement off, where nobody ever signs in: the
        // pre-accounts local name would otherwise just vanish from the UI, since
        // the account still carries the migration's placeholder.
        await this.adoptLegacyName()
        return
      }
      this.user = null
      this.status = 'signedOut'
    },

    /**
     * Redeem an invite and take the session. Throws on a bad invite so the
     * sign-in screen can show its own message — a 401 here is expected input,
     * not a broken session.
     */
    async signIn(inviteToken: string, deviceLabel?: string): Promise<void> {
      const label = (deviceLabel ?? '').trim() || inferDeviceLabel()
      const { user, session_token } = await redeemInvite(inviteToken, label)
      setSessionToken(session_token)
      this.user = user
      this.status = 'signedIn'
      this.endedReason = null

      await this.adoptLegacyName()
    },

    /**
     * The upgrade fold-in: someone who has been reading for months already has a
     * display name in this browser's localStorage — the LYCM-700 local "Profile"
     * that never reached the server. On their first real sign-in, carry it over
     * so "You've been reading as Ada" stays true, instead of silently resetting
     * them to the server's placeholder.
     *
     * Only ever *fills a gap*: if the account already carries a name someone
     * chose, the stale local label must not overwrite it.
     */
    async adoptLegacyName(): Promise<void> {
      const local = takeLegacyProfileName()
      if (!local || !this.user) return

      const current = this.user.display_name.trim()
      const isPlaceholder = current === '' || current === 'Reader' || current === this.user.email
      if (!isPlaceholder || current === local) return

      try {
        this.user = await renameApi(local)
      } catch {
        // Cosmetic. If it fails, the person keeps the server's name and can
        // rename in Settings — not worth blocking a successful sign-in over.
      }
    },

    /** Rename yourself; the name follows you to every device. */
    async rename(displayName: string): Promise<void> {
      this.user = await renameApi(displayName)
    },

    /** Sign out — this device only. */
    async signOut(): Promise<void> {
      try {
        await signOutApi()
      } finally {
        // Drop the local credential even if the revoke call failed: the person
        // asked to be signed out, and leaving a token behind that still opens the
        // library would be the worst possible outcome.
        setSessionToken('')
        this.user = null
        this.status = 'signedOut'
        this.endedReason = null
      }
    },

    /** A 401 came back from somewhere. Explain, don't just bounce. */
    sessionEnded(reason: SessionEndReason): void {
      if (this.status === 'signedOut') return // already handled; don't re-fire
      setSessionToken('')
      this.user = null
      this.status = 'signedOut'
      this.endedReason = reason
    },

    /** Dismiss the "you've been signed out" sheet without signing back in. */
    clearEnded(): void {
      this.endedReason = null
    },
  },
})

/** True when this device is carrying a session token (whether or not it works). */
export function hasStoredSession(): boolean {
  return getSessionToken() !== ''
}
