import { describe, expect, it } from 'vitest'
import {
  FONT_SIZES,
  clampFontSize,
  fontSizeCss,
  otherTheme,
  stepFontSize,
  themeStyles,
} from './theme'

describe('clampFontSize', () => {
  it('passes through allowed sizes', () => {
    expect(clampFontSize(100)).toBe(100)
    expect(clampFontSize(150)).toBe(150)
  })

  it('snaps to the nearest allowed size and clamps the ends', () => {
    expect(clampFontSize(96)).toBe(100)
    expect(clampFontSize(10)).toBe(FONT_SIZES[0])
    expect(clampFontSize(9999)).toBe(FONT_SIZES[FONT_SIZES.length - 1])
    expect(clampFontSize(Number.NaN)).toBe(FONT_SIZES[0])
  })
})

describe('stepFontSize', () => {
  it('moves up and down the ladder', () => {
    expect(stepFontSize(100, 1)).toBe(110)
    expect(stepFontSize(100, -1)).toBe(90)
  })

  it('clamps at both ends', () => {
    expect(stepFontSize(FONT_SIZES[0], -1)).toBe(FONT_SIZES[0])
    expect(stepFontSize(FONT_SIZES[FONT_SIZES.length - 1], 1)).toBe(
      FONT_SIZES[FONT_SIZES.length - 1],
    )
  })
})

describe('fontSizeCss', () => {
  it('renders a percent string', () => {
    expect(fontSizeCss(120)).toBe('120%')
  })
})

describe('theme helpers', () => {
  it('toggles between light and dark', () => {
    expect(otherTheme('light')).toBe('dark')
    expect(otherTheme('dark')).toBe('light')
  })

  it('produces distinct body styles per theme', () => {
    const light = themeStyles('light').body!.background
    const dark = themeStyles('dark').body!.background
    expect(light).not.toBe(dark)
    expect(themeStyles('dark').body!.color).toContain('!important')
  })
})
