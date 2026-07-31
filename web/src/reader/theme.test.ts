import { describe, expect, it } from 'vitest'
import {
  FONT_SIZES,
  LINE_SPACINGS,
  clampFontSize,
  fontSizeCss,
  isLineSpacingId,
  otherTheme,
  readerCss,
  resolveLineSpacing,
  stepFontSize,
  type ReaderStyleOptions,
} from './theme'

const BASE: ReaderStyleOptions = {
  theme: 'dark',
  fontSizePct: 100,
  lineSpacing: 'normal',
  fontFamily: null,
}

/** The declarations of one rule, by its exact selector. */
function ruleFor(css: string, selector: string): string {
  const match = css.match(
    new RegExp(`(^|\\n)${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`),
  )
  return match?.[2] ?? ''
}

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
})

describe('line spacing', () => {
  it('resolves an id to its metrics', () => {
    expect(resolveLineSpacing('tight').lineHeight).toBeLessThan(
      resolveLineSpacing('relaxed').lineHeight,
    )
    expect(LINE_SPACINGS.map((s) => s.id)).toEqual(['tight', 'normal', 'relaxed'])
  })

  it('falls back to normal for an unknown id', () => {
    expect(resolveLineSpacing('huge' as never).id).toBe('normal')
  })

  it('validates persisted values', () => {
    expect(isLineSpacingId('relaxed')).toBe(true)
    expect(isLineSpacingId('huge')).toBe(false)
    expect(isLineSpacingId(null)).toBe(false)
  })
})

describe('readerCss', () => {
  it('produces distinct body colours per theme', () => {
    const light = ruleFor(readerCss({ ...BASE, theme: 'light' }), 'body')
    const dark = ruleFor(readerCss({ ...BASE, theme: 'dark' }), 'body')
    expect(light).not.toBe(dark)
    expect(dark).toContain('!important')
  })

  // LYCM-110: the ladder only reached `body`, so a publisher that sizes its own
  // paragraphs won. The prose has to be pinned to `inherit` for the one figure
  // on `body` to mean anything.
  it('puts the size ladder on body and nowhere else', () => {
    const css = readerCss({ ...BASE, fontSizePct: 150 })
    expect(ruleFor(css, 'body')).toContain('font-size: 150% !important')

    const prose = css.match(/\n(p, div, span[^{]*)\{([^}]*)\}/)
    expect(prose?.[2]).toContain('font-size: inherit !important')
    // Any other font-size in the sheet must be relative (the heading ladder),
    // or the reader's setting stops reaching it.
    for (const decl of css.match(/font-size:[^;]+/g) ?? []) {
      expect(decl).toMatch(/inherit|%|em/)
    }
  })

  it('clamps an out-of-ladder size before emitting it', () => {
    expect(ruleFor(readerCss({ ...BASE, fontSizePct: 9999 }), 'body')).toContain(
      `font-size: ${FONT_SIZES[FONT_SIZES.length - 1]}%`,
    )
  })

  it('carries the chosen line spacing into the prose, not just body', () => {
    const relaxed = readerCss({ ...BASE, lineSpacing: 'relaxed' })
    const tight = readerCss({ ...BASE, lineSpacing: 'tight' })
    expect(ruleFor(relaxed, 'body')).toContain(
      `line-height: ${resolveLineSpacing('relaxed').lineHeight} !important`,
    )
    expect(relaxed.match(/line-height: 1\.85 !important/g)?.length).toBeGreaterThan(1)
    expect(relaxed).not.toBe(tight)
    // Paragraph gaps track the choice too — they are half the perceived spacing.
    expect(ruleFor(relaxed, 'p')).toContain(resolveLineSpacing('relaxed').paragraphGap)
  })

  // "Publisher" means the book's own faces, so the override must be absent
  // rather than merely inert.
  it('imposes a typeface only when one is chosen', () => {
    const publisher = readerCss({ ...BASE, fontFamily: null })
    expect(publisher).not.toContain('font-family')

    const serif = readerCss({ ...BASE, fontFamily: 'Georgia, serif' })
    expect(ruleFor(serif, 'body')).toContain('font-family: Georgia, serif !important')
    // ...and reaches past the publisher's own per-element faces.
    expect(serif).toContain('font-family: inherit !important')
  })

  // LYCM-106: an unbreakable token overflows the column box, which epub.js does
  // not clip, so the tail lands on top of the next page.
  it('lets long words break, and keeps figures inside the column', () => {
    const css = readerCss(BASE)
    expect(ruleFor(css, 'body')).toContain('overflow-wrap: break-word !important')
    expect(css.match(/overflow-wrap: break-word/g)?.length).toBeGreaterThan(1)
    expect(ruleFor(css, 'img, svg, video')).toContain('max-width: 100% !important')
    expect(ruleFor(css, 'pre')).toContain('white-space: pre-wrap !important')
  })

  it('leaves relatively-sized elements alone', () => {
    // sup/sub/small are meant to be smaller than their surroundings; forcing
    // them to inherit would flatten them.
    const prose = readerCss(BASE).match(/\n(p, div, span[^{]*)\{/)?.[1] ?? ''
    for (const tag of ['sup', 'sub', 'small']) {
      expect(prose.split(/,\s*/)).not.toContain(tag)
    }
  })
})
