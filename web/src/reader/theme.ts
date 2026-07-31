// Pure reader theme + typography state. Kept free of epub.js so it is unit
// testable; the composable injects the stylesheet this module builds into each
// rendered content document.

export type ReaderTheme = 'light' | 'dark'

/** Selectable font sizes, as percentages of the reader's base size. */
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

/** The size ladder as a CSS size string. */
export function fontSizeCss(pct: number): string {
  return `${clampFontSize(pct)}%`
}

/** The other theme — for a toggle. */
export function otherTheme(theme: ReaderTheme): ReaderTheme {
  return theme === 'light' ? 'dark' : 'light'
}

// ── Line spacing ────────────────────────────────────────────────────────────

export type LineSpacingId = 'tight' | 'normal' | 'relaxed'

export interface LineSpacing {
  id: LineSpacingId
  label: string
  /** Unitless line-height imposed on the prose. */
  lineHeight: number
  /** Gap between paragraphs, in em so it tracks the size ladder. */
  paragraphGap: string
}

export const LINE_SPACINGS: readonly LineSpacing[] = [
  { id: 'tight', label: 'Tight', lineHeight: 1.3, paragraphGap: '0.35em' },
  { id: 'normal', label: 'Normal', lineHeight: 1.55, paragraphGap: '0.6em' },
  { id: 'relaxed', label: 'Relaxed', lineHeight: 1.85, paragraphGap: '0.9em' },
]

export const DEFAULT_LINE_SPACING: LineSpacingId = 'normal'

/** Resolve a spacing id to its metrics, falling back to the default. */
export function resolveLineSpacing(id: LineSpacingId): LineSpacing {
  return LINE_SPACINGS.find((s) => s.id === id) ?? LINE_SPACINGS[1]!
}

/** Validate a persisted value before trusting it as a spacing id. */
export function isLineSpacingId(value: unknown): value is LineSpacingId {
  return LINE_SPACINGS.some((s) => s.id === value)
}

// ── Injected stylesheet ─────────────────────────────────────────────────────

export interface ReaderStyleOptions {
  theme: ReaderTheme
  /** Percentage from the size ladder. */
  fontSizePct: number
  lineSpacing: LineSpacingId
  /** Typeface to impose, or null to keep the book's own faces. */
  fontFamily: string | null
}

// The elements that actually carry the prose. Sizing only `body` is what left
// the ladder inert (LYCM-110): an element's own `font-size` declaration beats
// anything it would otherwise inherit, so a publisher stylesheet that sizes
// `p`/`div` — in `pt`, or in `em` that compounds down through nested divs —
// simply ignores the body. Forcing these back to `inherit` gives the ladder
// something to scale.
//
// Headings are handled separately (they keep a hierarchy, in em, so the ladder
// still reaches them). `sup`, `sub` and `small` are deliberately absent: they
// are supposed to be smaller than their surroundings.
const PROSE_SELECTOR = [
  'p',
  'div',
  'span',
  'a',
  'em',
  'strong',
  'i',
  'b',
  'u',
  'cite',
  'q',
  'li',
  'dd',
  'dt',
  'td',
  'th',
  'caption',
  'blockquote',
  'pre',
  'section',
  'article',
  'aside',
  'main',
  'header',
  'footer',
  'figcaption',
  'address',
].join(', ')

const HEADING_SELECTOR = 'h1, h2, h3, h4, h5, h6'

/** Heading ladder, in em so the reader's size setting still reaches them. */
const HEADING_SIZES: readonly (readonly [string, string])[] = [
  ['h1', '1.7em'],
  ['h2', '1.45em'],
  ['h3', '1.25em'],
  ['h4', '1.1em'],
  ['h5', '1em'],
  ['h6', '0.9em'],
]

/**
 * The stylesheet injected into the book's own content document. It is appended
 * last, so it wins ties against the publisher's stylesheets, and every rule
 * carries `!important` so it wins against their ordinary declarations too.
 *
 * Two jobs: paint the book in the app's theme, and normalize the typography a
 * publisher's CSS would otherwise dictate — size, line spacing, paragraph gaps
 * and word wrapping — so the reader's own controls mean something in every
 * book (LYCM-110), and no token can escape its column (LYCM-106).
 *
 * Note what is *not* here: `font-size` on the prose is `inherit`, never a
 * value, so the single figure on `body` remains the only lever.
 */
export function readerCss(opts: ReaderStyleOptions): string {
  // Background matches the app bg so the page text sits directly on the reading
  // surface (the design has no separate page card).
  const palette =
    opts.theme === 'dark'
      ? { color: '#d8d6cf', background: '#171717' }
      : { color: '#2c2925', background: '#f7f5f0' }
  const spacing = resolveLineSpacing(opts.lineSpacing)
  const family = opts.fontFamily

  // Long unbreakable tokens overflow the column box, which is not a clipping
  // boundary under epub.js's multi-column pagination — the tail is painted
  // over the *next* page. Breaking the word is the only fix (LYCM-106).
  const wrap = `  overflow-wrap: break-word !important;
  word-break: break-word !important;`

  return `/* Lyceum reader — injected into the book's own document, last in the cascade. */

/* Pin the base the size ladder is a percentage of, so 100% means the same
   thing in every book, and stop the Android WebView inflating text on its own. */
html {
  font-size: 100% !important;
  -webkit-text-size-adjust: 100% !important;
  text-size-adjust: 100% !important;
}

body {
  color: ${palette.color} !important;
  background: ${palette.background} !important;
  font-size: ${fontSizeCss(opts.fontSizePct)} !important;
  line-height: ${spacing.lineHeight} !important;
${family ? `  font-family: ${family} !important;\n` : ''}${wrap}
}

a {
  color: ${palette.color} !important;
}

${PROSE_SELECTOR} {
  font-size: inherit !important;
  line-height: ${spacing.lineHeight} !important;
${wrap}
}

${HEADING_SELECTOR} {
  line-height: 1.25 !important;
${wrap}
}

${HEADING_SIZES.map(([tag, size]) => `${tag} { font-size: ${size} !important; }`).join('\n')}

p {
  margin-top: ${spacing.paragraphGap} !important;
  margin-bottom: ${spacing.paragraphGap} !important;
}
${
  family
    ? `
/* Reach past the publisher's own per-element faces, for the same reason the
   size rules do. Omitted entirely under "Publisher", where the book's fonts
   are the point. */
${PROSE_SELECTOR}, ${HEADING_SELECTOR} {
  font-family: inherit !important;
}
`
    : ''
}
/* Figures and tables overflow the column the same way a long word does. */
img, svg, video {
  max-width: 100% !important;
  max-height: 100% !important;
}

table, pre {
  max-width: 100% !important;
}

/* Only wrapping keeps a long preformatted line on its own page. */
pre {
  white-space: pre-wrap !important;
}
`
}
