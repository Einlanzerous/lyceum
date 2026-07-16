// Parsing the invite out of whatever a person hands us (LYCM-88).
//
// The QR handoff encodes the invite as a `<origin>/sign-in?token=…` URL rather
// than a bare token, so a phone's native camera can just open it. That means the
// sign-in screen can receive either shape: the raw `lyc_…` key (pasted), or a
// full redemption URL (scanned, or a link someone forwarded). This normalises
// both to the token, or null when there's nothing token-shaped to redeem.

/** The prefix every invite/session token carries (see store.newToken). */
const TOKEN_PREFIX = 'lyc_'

/**
 * Pull the invite token out of a bare key or a `…/sign-in?token=…` URL.
 * Whitespace (from wrapped chat/log pastes) is stripped. Returns null when the
 * input isn't a plausible token.
 */
export function extractInviteToken(raw: string): string | null {
  let candidate = raw.trim()
  if (!candidate) return null

  // A scanned QR (or forwarded link) is a URL carrying ?token=. Anything that
  // doesn't parse as a URL is treated as the token itself.
  try {
    const fromQuery = new URL(candidate).searchParams.get('token')
    if (fromQuery !== null) candidate = fromQuery
  } catch {
    // Not a URL — fall through and treat the whole string as the token.
  }

  const token = candidate.replace(/\s+/g, '')
  return token.startsWith(TOKEN_PREFIX) && token.length > TOKEN_PREFIX.length ? token : null
}

/** Build the QR/redemption URL that carries an invite to another device. */
export function inviteSignInUrl(origin: string, token: string): string {
  return `${origin.replace(/\/$/, '')}/sign-in?token=${encodeURIComponent(token)}`
}
