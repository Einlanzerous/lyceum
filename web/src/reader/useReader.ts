// epub.js integration as a Vue composable. Owns the Book + Rendition lifecycle,
// exposes reactive UI state (loading/error/progress/nav bounds + chapter/page +
// title/author + TOC), and applies the app theme + font size to the rendered
// document. The rendering it drives runs in a real iframe and is not unit-
// testable under jsdom — see the reader smoke verification.
//
// The relocate callback and initial-CFI hooks let the reader view capture and
// restore reading position without reaching into epub.js internals.

import { onMounted, ref, shallowRef, watch, type Ref } from 'vue'
import ePub, { type Book, type Rendition } from 'epubjs'
import type { Location } from 'epubjs'
import type { NavItem } from 'epubjs'
import { bookFileUrl } from '@/api/client'
import { clampProgress } from '@/api/progress'
import { useTheme } from '@/theme'
import { FONT_SIZE_DEFAULT, fontSizeCss, stepFontSize, themeStyles } from './theme'
import { useReadingFont } from './readingFont'
import { resolveFontFamily } from './font'

/** Position information emitted on every relocate, for syncing. */
export interface RelocateInfo {
  cfi: string
  progress: number
}

export interface TocEntry {
  label: string
  href: string
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
  cfi: Ref<string>
  atStart: Ref<boolean>
  atEnd: Ref<boolean>
  fontSize: Ref<number>
  title: Ref<string>
  author: Ref<string>
  chapter: Ref<string>
  page: Ref<number>
  totalPages: Ref<number>
  toc: Ref<TocEntry[]>
  next(): void
  prev(): void
  increaseFont(): void
  decreaseFont(): void
  /** Jump to a TOC href. */
  goTo(href: string): void
  /** Current position, or null before the first render. For unload flush. */
  currentPosition(): RelocateInfo | null
  destroy(): void
}

const REGISTERED_THEME = 'lyceum'
const LOCATION_GRANULARITY = 1600

function flattenToc(items: NavItem[]): TocEntry[] {
  const out: TocEntry[] = []
  for (const item of items) {
    out.push({ label: (item.label ?? '').trim(), href: item.href })
    if (item.subitems?.length) out.push(...flattenToc(item.subitems))
  }
  return out
}

export function useReader(
  container: Ref<HTMLElement | null>,
  bookId: number,
  options: UseReaderOptions = {},
): ReaderControls {
  const { theme } = useTheme()
  const { font } = useReadingFont()

  const loading = ref(true)
  const error = ref<string | null>(null)
  const progress = ref(0)
  const cfi = ref('')
  const atStart = ref(true)
  const atEnd = ref(false)
  const fontSize = ref(FONT_SIZE_DEFAULT)
  const title = ref('')
  const author = ref('')
  const chapter = ref('')
  const page = ref(0)
  const totalPages = ref(0)
  const toc = ref<TocEntry[]>([])

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

  // Override the book's typeface, or remove the override to restore the
  // publisher's own fonts. epub.js re-applies overrides to each rendered
  // section, so this survives page turns; family and size stay independent.
  function applyFont(): void {
    const r = rendition.value
    if (!r) return
    const family = resolveFontFamily(font.value)
    if (family) r.themes.font(family)
    // removeOverride drops the inline font-family so the publisher's own fonts
    // win again. It ships in epub.js (themes.js) but is missing from the
    // bundled type defs, hence the cast.
    else
      (r.themes as typeof r.themes & { removeOverride(name: string): void }).removeOverride(
        'font-family',
      )
  }

  function recomputePage(): void {
    const b = book.value
    if (!b || totalPages.value <= 0 || !lastPosition?.cfi) return
    const pct = b.locations.percentageFromCfi(lastPosition.cfi)
    page.value = Math.min(totalPages.value, Math.max(1, Math.round(pct * totalPages.value)))
  }

  function chapterFor(href: string): string {
    const nav = book.value?.navigation
    if (!nav) return ''
    const direct = nav.get(href)
    if (direct?.label) return direct.label.trim()
    // Fall back to a prefix match against the flattened TOC (hrefs may carry
    // fragments the spine href doesn't).
    const base = href.split('#')[0]
    const hit = toc.value.find((t) => t.href.split('#')[0] === base)
    return hit?.label ?? ''
  }

  function onRelocated(loc: Location): void {
    atStart.value = loc.atStart
    atEnd.value = loc.atEnd
    const pct = loc.start.percentage
    if (typeof pct === 'number' && pct > 0) progress.value = clampProgress(pct)
    cfi.value = loc.start.cfi
    lastPosition = { cfi: loc.start.cfi, progress: progress.value }
    chapter.value = chapterFor(loc.start.href)
    recomputePage()
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
        // Single column — the host element is constrained to a centered reading
        // measure, so we never want epub.js's two-up spread.
        spread: 'none',
        allowScriptedContent: false,
      })
      rendition.value = r

      applyTheme()
      applyFontSize()
      applyFont()
      r.on('relocated', onRelocated)
      r.on('keyup', onKeyup)

      // Metadata + TOC for the chrome (best-effort, non-fatal).
      b.loaded.metadata
        .then((md) => {
          title.value = (md.title ?? '').trim()
          author.value = (md.creator ?? '').trim()
        })
        .catch(() => {})
      b.loaded.navigation
        .then((nav) => {
          toc.value = flattenToc(nav.toc ?? [])
          if (lastPosition?.cfi) chapter.value = chapterFor(cfi.value)
        })
        .catch(() => {})

      const startCfi = options.initialCfi ? await options.initialCfi() : null
      await r.display(startCfi ?? undefined)
      loading.value = false

      // Locations index → real page numbers + accurate progress.
      b.ready
        .then(() => b.locations.generate(LOCATION_GRANULARITY))
        .then(() => {
          totalPages.value = b.locations.length()
          if (lastPosition?.cfi) {
            progress.value = clampProgress(b.locations.percentageFromCfi(lastPosition.cfi))
            recomputePage()
          }
        })
        .catch(() => {})
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
  function goTo(href: string): void {
    void rendition.value?.display(href)
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

  // Re-theme the rendered document whenever the app theme flips.
  watch(theme, applyTheme)
  // Re-apply the typeface when the reading font is changed from Settings.
  watch(font, applyFont)

  onMounted(() => void load())

  return {
    loading,
    error,
    progress,
    cfi,
    atStart,
    atEnd,
    fontSize,
    title,
    author,
    chapter,
    page,
    totalPages,
    toc,
    next,
    prev,
    increaseFont,
    decreaseFont,
    goTo,
    currentPosition,
    destroy,
  }
}
