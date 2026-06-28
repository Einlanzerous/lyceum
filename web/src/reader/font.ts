// Pure reading-font catalog + resolver. Kept free of epub.js and Vue so it is
// unit testable; the composable applies the resolved family to the live
// rendition, and the reactive store (readingFont.ts) persists the choice.
//
// Default is "Publisher" — no override, so the book renders in its own
// typography. Curated faces are system stacks only: they resolve inside the
// epub.js iframe without bundling any @font-face. Size lives separately
// (theme.ts FONT_SIZES); family and size are independent.

export type ReadingFontId = 'publisher' | 'serif' | 'sans'

export interface ReadingFont {
  id: ReadingFontId
  label: string
  /** CSS font-family stack to apply, or null for the publisher default. */
  stack: string | null
  /** Short specimen note shown beside the choice in Settings. */
  hint: string
}

export const READING_FONTS: readonly ReadingFont[] = [
  { id: 'publisher', label: 'Publisher', stack: null, hint: "The book's own typography" },
  {
    id: 'serif',
    label: 'Serif',
    stack: "Georgia, 'Times New Roman', serif",
    hint: 'Georgia',
  },
  {
    id: 'sans',
    label: 'Sans',
    stack: "system-ui, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif",
    hint: 'System sans-serif',
  },
]

export const DEFAULT_READING_FONT: ReadingFontId = 'publisher'

/**
 * Resolve a font id to the CSS font-family to apply, or null when the book's
 * own fonts should win (publisher default, or an unknown id). Null is the
 * signal to remove any font override rather than set one.
 */
export function resolveFontFamily(id: ReadingFontId): string | null {
  return READING_FONTS.find((f) => f.id === id)?.stack ?? null
}

/** Validate a persisted value before trusting it as a font id. */
export function isReadingFontId(value: unknown): value is ReadingFontId {
  return READING_FONTS.some((f) => f.id === value)
}
