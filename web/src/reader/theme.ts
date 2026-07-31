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
// Containers count as much as the text elements inside them: `li` set to
// `inherit` still lands on whatever size its `ul` was given, so every wrapper a
// publisher might size — lists, tables, figures — has to be here too.
//
// Headings are handled separately (they keep a hierarchy, in em, so the ladder
// still reaches them). `sup`, `sub` and `small` are deliberately absent: they
// are supposed to be smaller than their surroundings.
const PROSE_ELEMENTS = [
  'p',
  'div',
  'span',
  'a',
  'em',
  'strong',
  'i',
  'b',
  'u',
  's',
  'cite',
  'q',
  'abbr',
  'center',
  'ul',
  'ol',
  'li',
  'dl',
  'dd',
  'dt',
  'table',
  'thead',
  'tbody',
  'tfoot',
  'tr',
  'td',
  'th',
  'caption',
  'blockquote',
  'figure',
  'figcaption',
  'address',
  'section',
  'article',
  'aside',
  'main',
  'nav',
  'header',
  'footer',
]

// Preformatted and code elements follow the size ladder like everything else,
// but keep whatever monospace face they were given: `pre` in a proportional
// typeface loses the column alignment that is the whole point of it.
const MONO_ELEMENTS = ['pre', 'code', 'kbd', 'samp', 'tt', 'var']

const HEADING_ELEMENTS = ['h1', 'h2', 'h3', 'h4', 'h5', 'h6']

/** Heading ladder, in em so the reader's size setting still reaches them. */
const HEADING_SIZES: readonly (readonly [string, string])[] = [
  ['h1', '1.7em'],
  ['h2', '1.45em'],
  ['h3', '1.25em'],
  ['h4', '1.1em'],
  ['h5', '1em'],
  ['h6', '0.9em'],
]

// `!important` alone only wins against a publisher's *ordinary* declarations.
// Against their important ones the cascade falls through to specificity, where
// a bare `p` (0,0,1) loses to anything class-scoped — `.chapter p
// { font-size: 9pt !important }` (0,1,1) — and the books that fight hardest are
// exactly the ones that write CSS like that. Two negations of an id nothing
// carries buy id-level specificity (2,0,1) while matching precisely the same
// elements.
const SPECIFICITY = ':not(#lyceum-reader):not(#lyceum-reader)'

/** One selector matching any of `elements`, outweighing publisher CSS. */
function selector(...elements: readonly string[][]): string {
  return `:is(${elements.flat().join(', ')})${SPECIFICITY}`
}

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
html${SPECIFICITY} {
  font-size: 100% !important;
  -webkit-text-size-adjust: 100% !important;
  text-size-adjust: 100% !important;
}

body${SPECIFICITY} {
  color: ${palette.color} !important;
  background: ${palette.background} !important;
  font-size: ${fontSizeCss(opts.fontSizePct)} !important;
  line-height: ${spacing.lineHeight} !important;
${family ? `  font-family: ${family} !important;\n` : ''}${wrap}
}

a${SPECIFICITY} {
  color: ${palette.color} !important;
}

${selector(PROSE_ELEMENTS, MONO_ELEMENTS)} {
  font-size: inherit !important;
  line-height: ${spacing.lineHeight} !important;
${wrap}
}

${selector(HEADING_ELEMENTS)} {
  line-height: 1.25 !important;
${wrap}
}

${HEADING_SIZES.map(([tag, size]) => `${tag}${SPECIFICITY} { font-size: ${size} !important; }`).join('\n')}

p${SPECIFICITY} {
  margin-top: ${spacing.paragraphGap} !important;
  margin-bottom: ${spacing.paragraphGap} !important;
}
${
  family
    ? `
/* Reach past the publisher's own per-element faces, for the same reason the
   size rules do. Omitted entirely under "Publisher", where the book's fonts
   are the point — and never applied to the monospace elements, whose face is
   load-bearing. */
${selector(PROSE_ELEMENTS, HEADING_ELEMENTS)} {
  font-family: inherit !important;
}
`
    : ''
}
/* Wide blocks overflow the column the same way a long word does. Note that
   'img' and 'svg' are absent deliberately: epub.js's own adjustImages hook
   already clamps them to the measured page height, and anything we add here
   outranks that clamp — a 'max-height: 100%' resolves to 'none' against an
   auto-height figure wrapper, which is how a tall image escapes the page. */
video${SPECIFICITY}, table${SPECIFICITY}, pre${SPECIFICITY} {
  max-width: 100% !important;
}

/* Only wrapping keeps a long preformatted line on its own page. */
pre${SPECIFICITY} {
  white-space: pre-wrap !important;
}
`
}
