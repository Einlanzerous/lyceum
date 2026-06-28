// epub.js integration as a Vue composable. Owns the Book + Rendition lifecycle,
// exposes reactive UI state (loading/error/progress/nav bounds), and folds in
// font-size + theme controls. The rendering it drives runs in a real iframe and
// is not unit-testable under jsdom — see the reader smoke verification.
//
// The relocate callback and initial-CFI hooks exist so LYCM-205 can capture and
// restore reading position without reaching into epub.js internals.

import { onMounted, ref, shallowRef, type Ref } from 'vue'
import ePub, { type Book, type Rendition } from 'epubjs'
import type { Location } from 'epubjs'
import { bookFileUrl } from '@/api/client'
import { clampProgress } from '@/api/progress'
import {
  FONT_SIZE_DEFAULT,
  fontSizeCss,
  otherTheme,
  stepFontSize,
  themeStyles,
  type ReaderTheme,
} from './theme'

/** Position information emitted on every relocate, for syncing. */
export interface RelocateInfo {
  cfi: string
  progress: number
}

export interface UseReaderOptions {
  /** Resolves the CFI to open at (e.g. a synced position); null = start. */
  initialCfi?: () => string | null | Promise<string | null>
  /** Called after every relocate (page turn, jump) with the new position. */
  onRelocate?: (info: RelocateInfo) => void
}

export interface ReaderControls {
  loading: Ref<boolean>
  error: Ref<string | null>
  progress: Ref<number>
  /** CFI of the current page; '' before the first render. */
  cfi: Ref<string>
  atStart: Ref<boolean>
  atEnd: Ref<boolean>
  fontSize: Ref<number>
  theme: Ref<ReaderTheme>
  next(): void
  prev(): void
  increaseFont(): void
  decreaseFont(): void
  toggleTheme(): void
  /** Current position, or null before the first render. For unload flush. */
  currentPosition(): RelocateInfo | null
  destroy(): void
}

const REGISTERED_THEME = 'lyceum'
// Density of the generated locations index; higher = finer progress, slower gen.
const LOCATION_GRANULARITY = 1600

export function useReader(
  container: Ref<HTMLElement | null>,
  bookId: number,
  options: UseReaderOptions = {},
): ReaderControls {
  const loading = ref(true)
  const error = ref<string | null>(null)
  const progress = ref(0)
  const cfi = ref('')
  const atStart = ref(true)
  const atEnd = ref(false)
  const fontSize = ref(FONT_SIZE_DEFAULT)
  const theme = ref<ReaderTheme>('light')

  const book = shallowRef<Book | null>(null)
  const rendition = shallowRef<Rendition | null>(null)
  let lastPosition: RelocateInfo | null = null

  function applyTheme(): void {
    const r = rendition.value
    if (!r) return
    r.themes.register(REGISTERED_THEME, themeStyles(theme.value))
    r.themes.select(REGISTERED_THEME)
  }

  function applyFontSize(): void {
    rendition.value?.themes.fontSize(fontSizeCss(fontSize.value))
  }

  function onRelocated(loc: Location): void {
    atStart.value = loc.atStart
    atEnd.value = loc.atEnd
    const pct = loc.start.percentage
    if (typeof pct === 'number' && pct > 0) progress.value = clampProgress(pct)
    cfi.value = loc.start.cfi
    lastPosition = { cfi: loc.start.cfi, progress: progress.value }
    options.onRelocate?.(lastPosition)
  }

  function onKeyup(event: KeyboardEvent): void {
    if (event.key === 'ArrowRight') next()
    else if (event.key === 'ArrowLeft') prev()
  }

  async function load(): Promise<void> {
    const host = container.value
    if (!host) {
      error.value = 'reader container not ready'
      loading.value = false
      return
    }
    try {
      const res = await fetch(bookFileUrl(bookId))
      if (!res.ok) throw new Error(`could not load book (${res.status})`)
      const buffer = await res.arrayBuffer()

      const b = ePub(buffer)
      book.value = b
      const r = b.renderTo(host, {
        width: '100%',
        height: '100%',
        flow: 'paginated',
        spread: 'auto',
        allowScriptedContent: false,
      })
      rendition.value = r

      applyTheme()
      applyFontSize()
      r.on('relocated', onRelocated)
      r.on('keyup', onKeyup) // key events originating inside the iframe

      const startCfi = options.initialCfi ? await options.initialCfi() : null
      await r.display(startCfi ?? undefined)
      loading.value = false

      // Build the locations index in the background for a real progress %.
      b.ready
        .then(() => b.locations.generate(LOCATION_GRANULARITY))
        .then(() => {
          const cfi = lastPosition?.cfi
          if (cfi) progress.value = clampProgress(b.locations.percentageFromCfi(cfi))
        })
        .catch(() => {
          /* progress stays at the relocate estimate; non-fatal */
        })
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'failed to open book'
      loading.value = false
    }
  }

  function next(): void {
    void rendition.value?.next()
  }
  function prev(): void {
    void rendition.value?.prev()
  }
  function increaseFont(): void {
    fontSize.value = stepFontSize(fontSize.value, 1)
    applyFontSize()
  }
  function decreaseFont(): void {
    fontSize.value = stepFontSize(fontSize.value, -1)
    applyFontSize()
  }
  function toggleTheme(): void {
    theme.value = otherTheme(theme.value)
    applyTheme()
  }
  function currentPosition(): RelocateInfo | null {
    return lastPosition
  }

  function destroy(): void {
    const r = rendition.value
    if (r) {
      r.off('relocated', onRelocated)
      r.off('keyup', onKeyup)
      r.destroy()
    }
    book.value?.destroy()
    rendition.value = null
    book.value = null
  }

  // Defer until the template's container element is in the DOM.
  onMounted(() => void load())

  return {
    loading,
    error,
    progress,
    cfi,
    atStart,
    atEnd,
    fontSize,
    theme,
    next,
    prev,
    increaseFont,
    decreaseFont,
    toggleTheme,
    currentPosition,
    destroy,
  }
}
