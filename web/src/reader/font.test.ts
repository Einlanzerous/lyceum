import { describe, expect, it } from 'vitest'
import {
  DEFAULT_READING_FONT,
  READING_FONTS,
  isReadingFontId,
  resolveFontFamily,
  type ReadingFontId,
} from './font'

describe('READING_FONTS catalog', () => {
  it('leads with the publisher default (no override)', () => {
    expect(READING_FONTS[0]!.id).toBe('publisher')
    expect(READING_FONTS[0]!.stack).toBeNull()
    expect(DEFAULT_READING_FONT).toBe('publisher')
  })

  it('offers at least a serif and a sans with real stacks', () => {
    const serif = READING_FONTS.find((f) => f.id === 'serif')
    const sans = READING_FONTS.find((f) => f.id === 'sans')
    expect(serif?.stack).toContain('Georgia')
    expect(sans?.stack).toContain('sans-serif')
  })
})

describe('resolveFontFamily', () => {
  it('returns null for the publisher default so the override is removed', () => {
    expect(resolveFontFamily('publisher')).toBeNull()
  })

  it('returns the css stack for a curated face', () => {
    expect(resolveFontFamily('serif')).toBe("Georgia, 'Times New Roman', serif")
  })

  it('falls back to null (publisher) for an unknown id', () => {
    expect(resolveFontFamily('bogus' as ReadingFontId)).toBeNull()
  })
})

describe('isReadingFontId', () => {
  it('accepts known ids and rejects everything else', () => {
    expect(isReadingFontId('publisher')).toBe(true)
    expect(isReadingFontId('serif')).toBe(true)
    expect(isReadingFontId('sans')).toBe(true)
    expect(isReadingFontId('comic-sans')).toBe(false)
    expect(isReadingFontId(null)).toBe(false)
    expect(isReadingFontId(undefined)).toBe(false)
    expect(isReadingFontId(3)).toBe(false)
  })
})
