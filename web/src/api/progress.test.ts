import { describe, expect, it } from 'vitest'
import { clampProgress, formatProgress } from './progress'

describe('clampProgress', () => {
  it('passes through valid fractions', () => {
    expect(clampProgress(0)).toBe(0)
    expect(clampProgress(0.42)).toBe(0.42)
    expect(clampProgress(1)).toBe(1)
  })

  it('clamps out-of-range and non-finite values', () => {
    expect(clampProgress(-0.5)).toBe(0)
    expect(clampProgress(2)).toBe(1)
    expect(clampProgress(Number.NaN)).toBe(0)
    expect(clampProgress(Number.POSITIVE_INFINITY)).toBe(1)
  })
})

describe('formatProgress', () => {
  it('renders a whole-percent label', () => {
    expect(formatProgress(0)).toBe('0%')
    expect(formatProgress(0.425)).toBe('43%')
    expect(formatProgress(1)).toBe('100%')
  })
})
