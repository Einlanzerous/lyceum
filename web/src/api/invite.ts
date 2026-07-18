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

// --- Pairing codes (LYCM-88) ---

/** Crockford base32 with the ambiguous glyphs removed — mirrors store.pairingAlphabet. */
const PAIRING_ALPHABET = '23456789ABCDEFGHJKMNPQRSTVWXYZ'
const PAIRING_CODE_LEN = 8

/** Fold a typed code to its canonical form: upper-cased, non-alphabet stripped. */
export function normalizePairingCode(raw: string): string {
  let out = ''
  for (const ch of raw.toUpperCase()) {
    if (PAIRING_ALPHABET.includes(ch)) out += ch
  }
  return out
}

/**
 * Whether the field holds a pairing code rather than a token, so the one sign-in
 * input can accept either. A `lyc_` token never looks like a code; a code reduces
 * to exactly PAIRING_CODE_LEN alphabet symbols.
 */
export function looksLikePairingCode(raw: string): boolean {
  if (raw.includes('lyc_')) return false
  return normalizePairingCode(raw).length === PAIRING_CODE_LEN
}

/** Pull a pairing code out of a bare code or a `…/sign-in?code=…` URL, or null. */
export function extractPairingCode(raw: string): string | null {
  let candidate = raw.trim()
  if (!candidate) return null
  try {
    const fromQuery = new URL(candidate).searchParams.get('code')
    if (fromQuery !== null) candidate = fromQuery
  } catch {
    // Not a URL — treat the whole string as the code.
  }
  const norm = normalizePairingCode(candidate)
  return norm.length === PAIRING_CODE_LEN ? norm : null
}
