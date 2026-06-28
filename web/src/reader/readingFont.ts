// Reactive reading-font preference: a single persisted source shared between
// Settings (where it's chosen) and the reader (where useReader applies it to
// the live rendition). Mirrors useTheme() in src/theme — a module-level ref,
// loaded from and saved to localStorage, so the choice survives reloads and
// carries across books. The resolution from id -> CSS lives in the pure
// helper (font.ts); this module only holds and persists the choice.

import { ref, watch } from 'vue'
import { DEFAULT_READING_FONT, isReadingFontId, type ReadingFontId } from './font'

const STORAGE_KEY = 'lyceum.readingFont'

function initialFont(): ReadingFontId {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isReadingFontId(saved)) return saved
  } catch {
    // localStorage unavailable (e.g. private mode): fall through to default.
  }
  return DEFAULT_READING_FONT
}

const font = ref<ReadingFontId>(initialFont())

function persist(value: ReadingFontId): void {
  try {
    localStorage.setItem(STORAGE_KEY, value)
  } catch {
    // best-effort persistence
  }
}

persist(font.value)
watch(font, persist)

export function useReadingFont() {
  return {
    font,
    set(value: ReadingFontId): void {
      font.value = value
    },
  }
}
