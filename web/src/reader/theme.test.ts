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

interface Rule {
  selector: string
  body: string
}

/** Every rule in the sheet, comments stripped. */
function rules(css: string): Rule[] {
  const stripped = css.replace(/\/\*[\s\S]*?\*\//g, '')
  return [...stripped.matchAll(/([^{}]+)\{([^}]*)\}/g)].map((m) => ({
    selector: m[1]!.trim(),
    body: m[2]!.trim(),
  }))
}

/** The declarations of the rule whose selector leads with `element`. */
function ruleFor(css: string, element: string): string {
  return rules(css).find((r) => r.selector.startsWith(element))?.body ?? ''
}

/** The rule carrying a given declaration, e.g. 'font-size: inherit'. */
function ruleDeclaring(css: string, declaration: string): Rule | undefined {
  return rules(css).find((r) => r.body.includes(declaration))
}

/** The element names a selector targets, out of its `:is(…)` list. */
function targets(selector: string): string[] {
  const list = selector.match(/^:is\(([^)]*)\)/)?.[1] ?? selector
  return list.split(',').map((s) => s.trim().split(':')[0]!.trim())
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
    expect(ruleDeclaring(css, 'font-size: inherit')).toBeDefined()
    // Any other font-size in the sheet must be relative (the heading ladder),
    // or the reader's setting stops reaching it.
    for (const decl of css.match(/font-size:[^;]+/g) ?? []) {
      expect(decl).toMatch(/inherit|%|em/)
    }
  })

  // Setting `li` to inherit is useless while its `ul` still carries the
  // publisher's absolute size — the containers have to be normalized too.
  it('normalizes the containers that size their contents', () => {
    const prose = targets(ruleDeclaring(readerCss(BASE), 'font-size: inherit')!.selector)
    for (const tag of ['ul', 'ol', 'dl', 'table', 'tbody', 'tr', 'figure', 'blockquote']) {
      expect(prose).toContain(tag)
    }
  })

  // `!important` alone still loses to a publisher's own important rule with any
  // class specificity, which is exactly what the worst books ship.
  it('outweighs class-scoped publisher rules', () => {
    for (const rule of rules(readerCss({ ...BASE, fontFamily: 'Georgia, serif' }))) {
      expect(rule.selector).toContain(':not(#')
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

  // A `pre` in Georgia has lost the column alignment that is the point of it,
  // and inline `code` beside it must not disagree.
  it('leaves the monospace elements their face, but not their size', () => {
    const serif = readerCss({ ...BASE, fontFamily: 'Georgia, serif' })
    const sized = targets(ruleDeclaring(serif, 'font-size: inherit')!.selector)
    const refaced = targets(ruleDeclaring(serif, 'font-family: inherit')!.selector)
    for (const tag of ['pre', 'code', 'kbd', 'samp']) {
      expect(sized).toContain(tag)
      expect(refaced).not.toContain(tag)
    }
  })

  // epub.js's own adjustImages hook clamps them to the measured page height.
  // Anything we add outranks it, and `max-height: 100%` computes to `none`
  // against an auto-height figure wrapper — so a tall plate escaped the page.
  it('leaves images to epub.js, which measures the page it has', () => {
    const css = readerCss(BASE)
    for (const rule of rules(css)) {
      expect(rule.body).not.toContain('max-height')
      expect(targets(rule.selector)).not.toContain('img')
      expect(targets(rule.selector)).not.toContain('svg')
    }
  })

  // LYCM-106: an unbreakable token overflows the column box, which epub.js does
  // not clip, so the tail lands on top of the next page.
  it('lets long words break, and keeps figures inside the column', () => {
    const css = readerCss(BASE)
    expect(ruleFor(css, 'body')).toContain('overflow-wrap: break-word !important')
    expect(css.match(/overflow-wrap: break-word/g)?.length).toBeGreaterThan(1)
    expect(ruleDeclaring(css, 'max-width: 100%')).toBeDefined()
    expect(ruleDeclaring(css, 'white-space: pre-wrap')).toBeDefined()
  })

  it('leaves relatively-sized elements alone', () => {
    // sup/sub/small are meant to be smaller than their surroundings; forcing
    // them to inherit would flatten them.
    const prose = targets(ruleDeclaring(readerCss(BASE), 'font-size: inherit')!.selector)
    for (const tag of ['sup', 'sub', 'small']) {
      expect(prose).not.toContain(tag)
    }
  })
})
