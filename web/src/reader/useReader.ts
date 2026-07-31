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
import type { Contents, Location } from 'epubjs'
import type { NavItem } from 'epubjs'
import { apiFetch } from '@/api/http'
import { clampProgress } from '@/api/progress'
import { useTheme } from '@/theme'
import { FONT_SIZE_DEFAULT, readerCss, stepFontSize } from './theme'
import { useReadingFont } from './readingFont'
import { useLineSpacing } from './lineSpacing'
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

const STYLE_ELEMENT_ID = 'lyceum-reader-style'
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
  const { lineSpacing } = useLineSpacing()

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

  // Theme, size, spacing and typeface all ride in one stylesheet we own and
  // rewrite in place, rather than epub.js's themes API. Its overrides land as
  // inline styles on `body` alone, which a publisher's own rules on `p`/`div`
  // defeat outright (LYCM-110), and its registered themes can only *add* rules
  // to a content document — never withdraw one, which "Publisher" needs when it
  // takes the typeface override back off.
  function styleOptions() {
    return {
      theme: theme.value,
      fontSizePct: fontSize.value,
      lineSpacing: lineSpacing.value,
      fontFamily: resolveFontFamily(font.value),
    }
  }

  /** Write (or rewrite) our stylesheet into one rendered content document. */
  function injectStyles(doc: Document): void {
    const head = doc.head ?? doc.documentElement
    if (!head) return
    let el = doc.getElementById(STYLE_ELEMENT_ID) as HTMLStyleElement | null
    if (!el) {
      el = doc.createElement('style')
      el.id = STYLE_ELEMENT_ID
      head.appendChild(el)
    }
    el.textContent = readerCss(styleOptions())
  }

  /** Re-style every section currently rendered, after a preference change. */
  function applyStyles(): void {
    const r = rendition.value
    if (!r) return
    // getContents() returns one Contents per rendered section; the bundled type
    // defs declare a single Contents, hence the cast.
    const contents = r.getContents() as unknown as Contents[]
    for (const c of contents) if (c.document) injectStyles(c.document)
  }

  function recomputePage(): void {
    const b = book.value
    // cfi.value tracks the current position on every relocate, including the
    // initial resume jump — unlike lastPosition, which stays null until we have
    // real (non-zero) progress. Keying off it lets the page number settle as
    // soon as the locations index exists.
    if (!b || totalPages.value <= 0 || !cfi.value) return
    const pct = b.locations.percentageFromCfi(cfi.value)
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
    chapter.value = chapterFor(loc.start.href)
    recomputePage()
    // Guard against persisting a spurious progress=0 position: epub.js reports
    // percentage 0 before it finishes paginating, which would otherwise save a
    // "start of book" position that clobbers real progress. Only persist once we
    // have real progress, or when genuinely at the very start of the book.
    if (progress.value > 0 || loc.atStart) {
      lastPosition = { cfi: loc.start.cfi, progress: progress.value }
      options.onRelocate?.(lastPosition)
    }
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
      // Through apiFetch, not bare fetch: the EPUB is a gated route now (LYCM-801)
      // and the Wails shell calls it cross-origin, where only the bearer header
      // travels. epub.js never touches the network — it is handed the bytes.
      const res = await apiFetch(`/books/${bookId}/file`)
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

      // Swipe-to-turn. The book text is rendered inside epub.js's own iframe,
      // so touches there never bubble to the outer reader surface. Register a
      // content hook to attach the swipe listener to each content document as
      // it's rendered (covers desktop/tablet touch; on phones the tap-zone
      // overlay handles it — see ReaderView).
      r.hooks.content.register((contents: Contents) => {
        const doc = contents.document
        // Every section is a fresh document, so it needs the stylesheet as it
        // is rendered — this is what carries the theme across page turns.
        injectStyles(doc)
        let sx = 0
        let sy = 0
        doc.addEventListener(
          'touchstart',
          (e: TouchEvent) => {
            sx = e.changedTouches[0]?.clientX ?? 0
            sy = e.changedTouches[0]?.clientY ?? 0
          },
          { passive: true },
        )
        doc.addEventListener(
          'touchend',
          (e: TouchEvent) => {
            const dx = (e.changedTouches[0]?.clientX ?? 0) - sx
            const dy = (e.changedTouches[0]?.clientY ?? 0) - sy
            if (Math.abs(dx) > 40 && Math.abs(dx) > Math.abs(dy)) {
              if (dx < 0) void r.next()
              else void r.prev()
            }
          },
          { passive: true },
        )
      })

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
          // Correct the progress bar once locations exist. On a mid-book resume
          // the initial relocate reports percentage 0 (pre-pagination), so the
          // bar sits at 0% until this runs. Key off cfi.value (always set by the
          // resume jump) rather than lastPosition, which is still null here
          // because that spurious 0 never cleared the persist guard.
          if (cfi.value) {
            progress.value = clampProgress(b.locations.percentageFromCfi(cfi.value))
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
    applyStyles()
  }
  function decreaseFont(): void {
    fontSize.value = stepFontSize(fontSize.value, -1)
    applyStyles()
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

  // Re-style the rendered sections whenever a reading preference changes — the
  // app theme, or the typeface / line spacing chosen here or in Settings.
  watch([theme, font, lineSpacing], applyStyles)

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
