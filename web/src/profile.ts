// What's left of the LYCM-700 local "Profile".
//
// Before accounts (LYCM-801) this was Lyceum's entire notion of identity: a
// display name in localStorage, never sent to the server, whose first letter was
// the library avatar. It is not identity any more — the account is (stores/auth).
//
// One job remains. Someone who has been reading for months has a name sitting in
// this browser, and the first time their server turns accounts on they have to
// sign in on a device they already own. "You've been reading as Ada" should stay
// true across that step — so the auth store reads the old label once, sends it up
// as the account's display name, and clears it. After that the name lives on the
// server and follows the person to every device.

const STORAGE_KEY = 'lyceum.profileName'

/**
 * Read *and consume* the pre-accounts local display name, or '' when there isn't
 * one.
 *
 * Clearing on read is what makes the fold-in happen exactly once. If it lingered,
 * a name the person later changed on the server could be quietly reverted by the
 * stale local label the next time they signed in on this browser.
 */
export function takeLegacyProfileName(): string {
  try {
    const name = (localStorage.getItem(STORAGE_KEY) ?? '').trim()
    localStorage.removeItem(STORAGE_KEY)
    return name
  } catch {
    // localStorage unavailable (private mode): nothing to carry over.
    return ''
  }
}

/**
 * Peek without consuming, so the sign-in screen can greet a returning reader by
 * name ("You've been reading as Ada") *before* they have signed in — the point at
 * which the name is still the only thing we know about them.
 */
export function peekLegacyProfileName(): string {
  try {
    return (localStorage.getItem(STORAGE_KEY) ?? '').trim()
  } catch {
    return ''
  }
}
