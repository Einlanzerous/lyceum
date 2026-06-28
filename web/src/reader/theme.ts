// Pure reader theme + font-size state. Kept free of epub.js so it is unit
// testable; the composable applies these values to the live rendition.

export type ReaderTheme = 'light' | 'dark'

/** Selectable font sizes, as percentages of the publisher default. */
export const FONT_SIZES = [80, 90, 100, 110, 120, 130, 150, 175, 200] as const
export const FONT_SIZE_DEFAULT = 100

/** Snap an arbitrary percentage onto the nearest allowed font size. */
export function clampFontSize(pct: number): number {
  const min = FONT_SIZES[0]
  const max = FONT_SIZES[FONT_SIZES.length - 1]
  if (!Number.isFinite(pct) || pct <= min) return min
  if (pct >= max) return max
  return FONT_SIZES.reduce((best, size) =>
    Math.abs(size - pct) < Math.abs(best - pct) ? size : best,
  )
}

/** Move one step up (dir=1) or down (dir=-1) the font-size ladder, clamped. */
export function stepFontSize(current: number, dir: 1 | -1): number {
  const idx = FONT_SIZES.indexOf(clampFontSize(current) as (typeof FONT_SIZES)[number])
  const next = Math.min(FONT_SIZES.length - 1, Math.max(0, idx + dir))
  return FONT_SIZES[next]!
}

/** epub.js themes.fontSize() wants a CSS size string. */
export function fontSizeCss(pct: number): string {
  return `${clampFontSize(pct)}%`
}

/** The other theme — for a toggle. */
export function otherTheme(theme: ReaderTheme): ReaderTheme {
  return theme === 'light' ? 'dark' : 'light'
}

/**
 * epub.js theme rule object applied to the rendered document body. Targets the
 * iframe content, not the host app, so it must set its own colors.
 */
export function themeStyles(theme: ReaderTheme): Record<string, Record<string, string>> {
  const palette =
    theme === 'dark'
      ? { color: '#e6e1d8', background: '#16140f' }
      : { color: '#1c1a17', background: '#f7f5f0' }
  return {
    body: {
      color: `${palette.color} !important`,
      background: `${palette.background} !important`,
    },
    a: { color: `${palette.color} !important` },
  }
}
