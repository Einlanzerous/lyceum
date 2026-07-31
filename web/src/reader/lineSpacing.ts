// Reactive line-spacing preference (LYCM-110): a single persisted source shared
// between the reader popover and Settings. Mirrors readingFont.ts — a
// module-level ref loaded from and saved to localStorage, so the choice
// survives reloads and carries across books. The metrics behind each id live in
// the pure helper (theme.ts); this module only holds and persists the choice.

import { ref, watch } from 'vue'
import { DEFAULT_LINE_SPACING, isLineSpacingId, type LineSpacingId } from './theme'

const STORAGE_KEY = 'lyceum.lineSpacing'

function initialSpacing(): LineSpacingId {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isLineSpacingId(saved)) return saved
  } catch {
    // localStorage unavailable (e.g. private mode): fall through to default.
  }
  return DEFAULT_LINE_SPACING
}

const lineSpacing = ref<LineSpacingId>(initialSpacing())

function persist(value: LineSpacingId): void {
  try {
    localStorage.setItem(STORAGE_KEY, value)
  } catch {
    // best-effort persistence
  }
}

persist(lineSpacing.value)
watch(lineSpacing, persist)

export function useLineSpacing() {
  return {
    lineSpacing,
    set(value: LineSpacingId): void {
      lineSpacing.value = value
    },
  }
}
