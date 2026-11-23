import { describe, expect, it } from 'vitest'
import { isPlainEnter } from './keys'

describe('isPlainEnter', () => {
  it('accepts a real Enter', () => {
    expect(isPlainEnter(new KeyboardEvent('keydown', { key: 'Enter' }))).toBe(true)
  })

  it('rejects the Enter that confirms an IME composition', () => {
    expect(isPlainEnter(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true }))).toBe(
      false,
    )
    // Safari: composition already ended by the time keydown fires, keyCode 229.
    expect(isPlainEnter(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229 }))).toBe(false)
  })

  it('rejects other keys', () => {
    expect(isPlainEnter(new KeyboardEvent('keydown', { key: 'a' }))).toBe(false)
  })
})
