// Keyboard helpers (LYCM-142).

/**
 * True for an Enter that means "submit", false for the Enter that confirms an IME
 * composition (Japanese, Chinese, Korean input). That one arrives as a keydown
 * with `isComposing` set — Safari reports it with `keyCode` 229 and a false
 * `isComposing` — and acting on it submits half-typed text.
 *
 * Use it on `keydown`, not `keyup`: the keyup of a composition-confirming Enter
 * lands *after* composition has ended and looks exactly like a real one.
 */
export function isPlainEnter(e: KeyboardEvent): boolean {
  return e.key === 'Enter' && !e.isComposing && e.keyCode !== 229
}
