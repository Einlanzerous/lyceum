<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useReader } from '@/reader/useReader'
import { createPositionSync } from '@/reader/sync'
import { useUnloadFlush } from '@/reader/useUnloadFlush'
import { getBook, putPositionKeepalive, setBookFinished } from '@/api/client'
import { formatProgress } from '@/api/progress'
import { useTheme, type Theme } from '@/theme'
import { useReadingFont } from '@/reader/readingFont'
import { READING_FONTS, resolveFontFamily } from '@/reader/font'

const props = defineProps<{ id: string }>()
const router = useRouter()
const bookId = computed(() => Number(props.id))

const container = ref<HTMLElement | null>(null)
const chromeHidden = ref(false)
const tocOpen = ref(false)
const settingsOpen = ref(false)

// Reading preferences, shared with the Settings page via the same persisted
// stores — changing them here also updates Settings, and vice versa.
const { theme, set: setTheme } = useTheme()
const { font, set: setFont } = useReadingFont()
const themeOptions: { value: Theme; label: string }[] = [
  { value: 'dark', label: 'Dark' },
  { value: 'light', label: 'Light' },
]
const specimenFamily = computed(() => resolveFontFamily(font.value) ?? 'var(--font-read)')

// Finished (mark-as-read) state, fetched once so the gear shows the right label.
const finished = ref(false)
getBook(bookId.value)
  .then((b) => {
    finished.value = b.finished === true
  })
  .catch(() => {})
async function toggleFinished(): Promise<void> {
  const next = !finished.value
  finished.value = next // optimistic
  try {
    await setBookFinished(bookId.value, next)
  } catch {
    finished.value = !next // roll back
  }
}

const resumedVisible = ref(false)
let resumedTimer: ReturnType<typeof setTimeout> | undefined

// Position sync: restore on open, debounce-save each turn, flush on leave.
const sync = createPositionSync(bookId.value, { putKeepalive: putPositionKeepalive })
const didResume = ref(false)
const reader = useReader(container, bookId.value, {
  initialCfi: async () => {
    const cfi = await sync.restore()
    didResume.value = !!cfi
    return cfi
  },
  onRelocate: (info) => sync.schedule(info),
})

const headerTitle = computed(() => reader.title.value || `Book ${props.id}`)
const pageLabel = computed(() =>
  reader.totalPages.value > 0 ? `page ${reader.page.value} of ${reader.totalPages.value}` : '',
)

// Show the "resumed" affordance once the book has rendered at a saved place.
watch(
  () => reader.loading.value,
  (isLoading) => {
    if (!isLoading && didResume.value) {
      resumedVisible.value = true
      clearTimeout(resumedTimer)
      resumedTimer = setTimeout(() => (resumedVisible.value = false), 4000)
    }
  },
)

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'ArrowRight') reader.next()
  else if (event.key === 'ArrowLeft') reader.prev()
  else if (event.key === 'Escape') {
    if (settingsOpen.value) settingsOpen.value = false
    else if (tocOpen.value) tocOpen.value = false
    else void router.push('/')
  }
}

// Horizontal swipe → page turn. Tracks Y too so vertical scrolls/drags don't
// count as page turns.
let touchStartX = 0
let touchStartY = 0
function onTouchStart(event: TouchEvent): void {
  touchStartX = event.changedTouches[0]?.clientX ?? 0
  touchStartY = event.changedTouches[0]?.clientY ?? 0
}
function onTouchEnd(event: TouchEvent): void {
  const dx = (event.changedTouches[0]?.clientX ?? 0) - touchStartX
  const dy = (event.changedTouches[0]?.clientY ?? 0) - touchStartY
  if (Math.abs(dx) < 40 || Math.abs(dx) < Math.abs(dy)) return
  if (dx < 0) reader.next()
  else reader.prev()
}

// The mobile tap zones overlay the epub.js iframe, so a swipe there never
// reaches the reading surface. Handle both here: a horizontal swipe turns the
// page; a tap acts by horizontal zone (prev / toggle chrome / next).
function onZoneTouchEnd(event: TouchEvent): void {
  const dx = (event.changedTouches[0]?.clientX ?? 0) - touchStartX
  const dy = (event.changedTouches[0]?.clientY ?? 0) - touchStartY
  if (Math.abs(dx) > 40 && Math.abs(dx) > Math.abs(dy)) {
    if (dx < 0) reader.next()
    else reader.prev()
    return
  }
  if (Math.abs(dx) > 12 || Math.abs(dy) > 12) return // a drag, not a tap
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  const frac = (touchStartX - rect.left) / rect.width
  if (frac < 0.32) reader.prev()
  else if (frac > 0.68) reader.next()
  else chromeHidden.value = !chromeHidden.value
}

function openToc(): void {
  settingsOpen.value = false
  tocOpen.value = true
}
function toggleSettings(): void {
  tocOpen.value = false
  settingsOpen.value = !settingsOpen.value
}
function goTo(href: string): void {
  reader.goTo(href)
  tocOpen.value = false
}

useUnloadFlush(() => {
  const pos = reader.currentPosition()
  if (pos) sync.flushOnUnload(pos)
})

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  clearTimeout(resumedTimer)
  void sync.flush()
  sync.dispose()
  reader.destroy()
})
</script>

<template>
  <div
    class="reader"
    :class="{ 'reader--chromeless': chromeHidden }"
    :data-cfi="reader.cfi.value"
    :data-progress="reader.progress.value"
  >
    <!-- Top bar -->
    <header class="rbar">
      <button type="button" class="pill" @click="router.push('/')">
        <span>←</span><span class="pill__label">Library</span>
      </button>

      <div class="rbar__center">
        <div class="rbar__title">{{ headerTitle }}</div>
        <div v-if="reader.author.value" class="rbar__author">{{ reader.author.value }}</div>
      </div>

      <div class="rbar__right">
        <button
          type="button"
          class="circle"
          :class="{ 'is-active': settingsOpen }"
          aria-label="Reading settings"
          aria-haspopup="dialog"
          :aria-expanded="settingsOpen"
          @click="toggleSettings()"
        >
          ⚙
        </button>
        <button type="button" class="circle" aria-label="Contents" @click="openToc()">☰</button>
      </div>
    </header>

    <!-- Reading settings popover (font size · theme · reading font) -->
    <Transition name="pop">
      <div v-if="settingsOpen" class="rset">
        <div class="rset__scrim" @click="settingsOpen = false" />
        <div class="rset__panel" role="dialog" aria-label="Reading settings">
          <div class="rset__row">
            <span class="rset__label">Text size</span>
            <div class="rset__size">
              <button type="button" aria-label="Smaller text" @click="reader.decreaseFont()">
                A−
              </button>
              <span class="rset__val">{{ reader.fontSize.value }}%</span>
              <button
                type="button"
                class="rset__up"
                aria-label="Larger text"
                @click="reader.increaseFont()"
              >
                A+
              </button>
            </div>
          </div>

          <div class="rset__row">
            <span class="rset__label">Theme</span>
            <div class="seg" role="group" aria-label="Theme">
              <button
                v-for="opt in themeOptions"
                :key="opt.value"
                type="button"
                class="seg__btn"
                :class="{ 'is-active': theme === opt.value }"
                @click="setTheme(opt.value)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>

          <div class="rset__row rset__row--stack">
            <span class="rset__label">Font</span>
            <div class="seg" role="group" aria-label="Reading font">
              <button
                v-for="opt in READING_FONTS"
                :key="opt.id"
                type="button"
                class="seg__btn"
                :class="{ 'is-active': font === opt.id }"
                :title="opt.hint"
                @click="setFont(opt.id)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>

          <p class="rset__specimen" :style="{ fontFamily: specimenFamily }">
            The quick brown fox jumps over the lazy dog.
          </p>

          <div class="rset__row rset__row--stack">
            <span class="rset__label">This book</span>
            <button
              type="button"
              class="rset__finish"
              :class="{ 'is-done': finished }"
              @click="toggleFinished()"
            >
              {{ finished ? '✓ Read — mark as unread' : 'Mark as read' }}
            </button>
          </div>
        </div>
      </div>
    </Transition>

    <!-- Reading surface (epub.js iframe) -->
    <div
      ref="container"
      class="reader__surface"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onTouchEnd"
    ></div>

    <!-- Desktop side nav -->
    <button
      type="button"
      class="snav snav--prev"
      :disabled="reader.atStart.value"
      aria-label="Previous page"
      @click="reader.prev()"
    >
      ‹
    </button>
    <button
      type="button"
      class="snav snav--next"
      :disabled="reader.atEnd.value"
      aria-label="Next page"
      @click="reader.next()"
    >
      ›
    </button>

    <!-- Mobile tap zones. Touch is handled on the container so a swipe turns
         the page and a tap acts by zone (prev / toggle chrome / next). -->
    <div
      class="tapzones"
      aria-hidden="true"
      @touchstart.passive="onTouchStart"
      @touchend.passive="onZoneTouchEnd"
    >
      <span class="tapzones__z tapzones__z--prev" />
      <span class="tapzones__z tapzones__z--mid" />
      <span class="tapzones__z tapzones__z--next" />
    </div>

    <!-- Bottom progress -->
    <footer class="rprog">
      <div class="rprog__meta">
        <span class="rprog__chapter">{{ reader.chapter.value || 'Reading' }}</span>
        <span class="rprog__nums">
          <span class="rprog__pct">{{ formatProgress(reader.progress.value) }}</span>
          <template v-if="pageLabel"> · {{ pageLabel }}</template>
        </span>
      </div>
      <div class="rprog__track">
        <div class="rprog__fill" :style="{ width: reader.progress.value * 100 + '%' }" />
      </div>
    </footer>

    <!-- Opening overlay -->
    <div v-if="reader.loading.value" class="overlay">
      <div class="overlay__icon"><span /></div>
      <div class="overlay__title">Opening {{ headerTitle }}…</div>
      <div class="overlay__sub">Finding your place</div>
    </div>

    <!-- Error overlay -->
    <div v-else-if="reader.error.value" class="overlay overlay--error" role="alert">
      <div class="overlay__bang">!</div>
      <div class="overlay__title">This book won't open</div>
      <div class="overlay__sub">
        The EPUB may be damaged or unsupported. Your reading place is safe.
      </div>
      <div class="overlay__actions">
        <button type="button" class="btn btn--brass" @click="router.go(0)">↻ Retry</button>
        <button type="button" class="btn btn--ghost" @click="router.push('/')">← Library</button>
      </div>
    </div>

    <!-- Resumed affordance -->
    <Transition name="resumed">
      <div v-if="resumedVisible" class="resumed">
        <span class="resumed__dot" />
        <span class="resumed__text">Resumed where you left off</span>
        <span v-if="reader.page.value > 0" class="resumed__page"
          >· page {{ reader.page.value }}</span
        >
      </div>
    </Transition>

    <!-- TOC drawer -->
    <Transition name="drawer">
      <div v-if="tocOpen" class="toc">
        <div class="toc__scrim" @click="tocOpen = false" />
        <aside class="toc__panel">
          <div class="toc__head">
            <span>Contents</span>
            <button type="button" class="circle" aria-label="Close" @click="tocOpen = false">
              ✕
            </button>
          </div>
          <nav class="toc__list">
            <button
              v-for="(entry, i) in reader.toc.value"
              :key="i"
              type="button"
              class="toc__item"
              @click="goTo(entry.href)"
            >
              {{ entry.label || 'Untitled' }}
            </button>
            <p v-if="reader.toc.value.length === 0" class="toc__empty">No chapters listed.</p>
          </nav>
        </aside>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.reader {
  position: relative;
  height: 100%;
  background: var(--bg);
  overflow: hidden;
}

/* ── Top bar ── */
.rbar {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 18px 28px;
  padding-top: max(18px, env(safe-area-inset-top));
  background: linear-gradient(var(--bg) 40%, transparent);
  transition:
    opacity 0.2s ease,
    transform 0.2s ease;
}
.reader--chromeless .rbar {
  opacity: 0;
  transform: translateY(-100%);
  pointer-events: none;
}
.rbar__center {
  margin: 0 auto;
  text-align: center;
  min-width: 0;
}
.rbar__title {
  font: 700 14px var(--font-display);
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 46vw;
}
.rbar__author {
  font: 400 11.5px var(--font-ui);
  color: var(--dim);
  margin-top: 1px;
}
.rbar__right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pill {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 15px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  color: var(--reading);
  font: 600 13px var(--font-ui);
  cursor: pointer;
}
.pill > span:first-child {
  font-size: 15px;
}
@media (max-width: 760px) {
  .pill__label {
    display: none;
  }
  .rbar__title {
    max-width: 56vw;
  }
}

.circle {
  width: 38px;
  height: 38px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  color: var(--muted);
  font-size: 16px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}
.circle.is-active {
  border-color: rgba(201, 154, 78, 0.4);
  background: rgba(201, 154, 78, 0.14);
  color: var(--brass-bright);
}

/* ── Reading settings popover ── */
.rset {
  position: absolute;
  inset: 0;
  z-index: 7;
}
.reader--chromeless .rset {
  opacity: 0;
  pointer-events: none;
}
.rset__scrim {
  position: absolute;
  inset: 0;
}
.rset__panel {
  position: absolute;
  top: 62px;
  right: clamp(16px, 4vw, 28px);
  width: min(304px, calc(100vw - 32px));
  padding: 6px 16px 16px;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface-raised) 94%, transparent);
  backdrop-filter: blur(12px);
  box-shadow: var(--shadow-pop);
}
.rset__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 12px 0;
}
.rset__row + .rset__row {
  border-top: 1px solid var(--border);
}
.rset__row--stack {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}
.rset__label {
  font: 600 13px var(--font-ui);
  color: var(--text);
}
.rset__size {
  display: flex;
  align-items: center;
  gap: 4px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  overflow: hidden;
}
.rset__size button {
  padding: 7px 13px;
  border: none;
  background: transparent;
  color: var(--muted);
  font: 600 13px var(--font-ui);
  cursor: pointer;
}
.rset__size .rset__up {
  color: var(--text);
  font-weight: 700;
  font-size: 15px;
}
.rset__val {
  min-width: 42px;
  text-align: center;
  font: 600 12px var(--font-ui);
  color: var(--dim);
}

/* segmented control — mirrors the Settings page */
.seg {
  display: flex;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  overflow: hidden;
  flex: none;
}
.rset__row--stack .seg {
  width: 100%;
}
.seg__btn {
  flex: 1;
  padding: 8px 14px;
  border: none;
  background: transparent;
  color: var(--dim);
  font: 700 12.5px var(--font-ui);
  cursor: pointer;
  white-space: nowrap;
}
.seg__btn.is-active {
  background: var(--brass);
  color: var(--on-brass);
}
.rset__specimen {
  margin: 12px 0 0;
  font-size: 16px;
  line-height: 1.5;
  color: var(--reading);
}
.rset__finish {
  width: 100%;
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--text);
  font: 700 13px var(--font-ui);
  cursor: pointer;
}
.rset__finish:hover {
  border-color: var(--brass);
}
.rset__finish.is-done {
  border-color: var(--success);
  color: var(--success);
}

.pop-enter-active {
  animation: lycFade 0.16s ease both;
}
.pop-leave-active {
  transition: opacity 0.14s ease;
}
.pop-leave-to {
  opacity: 0;
}

/* ── Reading surface ── a centered reading measure, inset from the chrome. */
.reader__surface {
  position: absolute;
  top: 64px;
  bottom: 64px;
  left: 50%;
  transform: translateX(-50%);
  width: min(720px, calc(100% - 140px));
}
@media (max-width: 760px) {
  .reader__surface {
    top: 56px;
    bottom: 66px;
    width: calc(100% - 40px);
  }
}

/* ── Side nav ── */
.snav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  z-index: 4;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  border: 1px solid var(--border-strong);
  background: color-mix(in srgb, var(--surface) 60%, transparent);
  backdrop-filter: blur(6px);
  color: var(--reading);
  font-size: 22px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: opacity 0.2s ease;
}
.snav--prev {
  left: 26px;
}
.snav--next {
  right: 26px;
  border-color: rgba(201, 154, 78, 0.3);
  background: rgba(201, 154, 78, 0.12);
  color: var(--brass-bright);
}
.snav:disabled {
  opacity: 0.25;
  cursor: default;
}
.reader--chromeless .snav {
  opacity: 0;
  pointer-events: none;
}

/* ── Mobile tap zones ── */
.tapzones {
  display: none;
}
@media (max-width: 760px) {
  .snav {
    display: none;
  }
  .tapzones {
    display: flex;
    position: absolute;
    left: 0;
    right: 0;
    top: 60px;
    bottom: 70px;
    z-index: 3;
  }
  .tapzones__z {
    border: none;
    background: transparent;
    cursor: pointer;
  }
  .tapzones__z--prev {
    flex: 1;
  }
  .tapzones__z--mid {
    flex: 1.1;
  }
  .tapzones__z--next {
    flex: 1;
  }
}

/* ── Bottom progress ── */
.rprog {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  z-index: 5;
  padding: 18px 40px 22px;
  padding-bottom: max(22px, env(safe-area-inset-bottom));
  background: linear-gradient(transparent, var(--bg) 60%);
  transition:
    opacity 0.2s ease,
    transform 0.2s ease;
}
.reader--chromeless .rprog {
  opacity: 0;
  transform: translateY(100%);
  pointer-events: none;
}
.rprog__meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font: 500 11.5px var(--font-ui);
  color: var(--dim);
  margin-bottom: 9px;
}
.rprog__chapter {
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rprog__nums {
  white-space: nowrap;
}
.rprog__pct {
  color: var(--brass-bright);
  font-weight: 700;
}
.rprog__track {
  height: 3px;
  border-radius: 2px;
  background: var(--border-strong);
}
.rprog__fill {
  height: 100%;
  border-radius: 2px;
  background: var(--brass);
  transition: width 0.18s ease;
}

/* ── Overlays ── */
.overlay {
  position: absolute;
  inset: 0;
  z-index: 6;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 0 34px;
  background: color-mix(in srgb, var(--bg) 88%, transparent);
  backdrop-filter: blur(2px);
}
.overlay__icon {
  width: 34px;
  height: 46px;
  border-radius: 4px;
  border: 2px solid rgba(201, 154, 78, 0.5);
  position: relative;
  margin-bottom: 18px;
}
.overlay__icon span {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--brass);
  animation: lycPulse 1.3s ease-in-out infinite;
}
.overlay__bang {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1.5px solid rgba(224, 138, 110, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  font: 400 24px var(--font-ui);
  color: var(--error);
  margin-bottom: 18px;
}
.overlay__title {
  font: 800 19px var(--font-display);
  color: var(--text);
}
.overlay__sub {
  font: 400 13px/1.5 var(--font-ui);
  color: var(--muted);
  margin-top: 7px;
  max-width: 320px;
}
.overlay__actions {
  display: flex;
  gap: 9px;
  margin-top: 18px;
}
.btn {
  padding: 9px 16px;
  border-radius: 999px;
  font: 700 12.5px var(--font-ui);
  cursor: pointer;
}
.btn--brass {
  border: none;
  background: var(--brass);
  color: var(--on-brass);
}
.btn--ghost {
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--text);
}

/* ── Resumed affordance ── */
.resumed {
  position: absolute;
  left: 50%;
  bottom: 70px;
  transform: translateX(-50%);
  z-index: 7;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 10px 16px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--surface-raised) 88%, transparent);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(201, 154, 78, 0.28);
  box-shadow: var(--shadow-pop);
  white-space: nowrap;
}
.resumed__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--brass);
}
.resumed__text {
  font: 600 12.5px var(--font-ui);
  color: var(--text);
}
.resumed__page {
  font: 500 12px var(--font-ui);
  color: var(--muted);
}
.resumed-enter-active {
  animation: lycFade 0.4s ease both;
}
.resumed-leave-active {
  transition: opacity 0.3s ease;
}
.resumed-leave-to {
  opacity: 0;
}

/* ── TOC drawer ── */
.toc {
  position: absolute;
  inset: 0;
  z-index: 8;
}
.toc__scrim {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
}
.toc__panel {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(340px, 84vw);
  background: var(--surface);
  border-left: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  box-shadow: -12px 0 40px rgba(0, 0, 0, 0.4);
}
.toc__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 20px 20px 14px;
  font: 800 16px var(--font-display);
  color: var(--text);
}
.toc__list {
  flex: 1;
  overflow-y: auto;
  padding: 0 12px 20px;
}
.toc__item {
  display: block;
  width: 100%;
  text-align: left;
  padding: 11px 14px;
  border: none;
  background: transparent;
  border-radius: 8px;
  color: var(--reading);
  font: 500 13.5px var(--font-ui);
  cursor: pointer;
}
.toc__item:hover {
  background: var(--surface-raised);
  color: var(--text);
}
.toc__empty {
  padding: 14px;
  color: var(--muted);
  font: 400 13px var(--font-ui);
}

.drawer-enter-active .toc__panel,
.drawer-leave-active .toc__panel {
  transition: transform 0.22s ease;
}
.drawer-enter-from .toc__panel,
.drawer-leave-to .toc__panel {
  transform: translateX(100%);
}
.drawer-enter-active .toc__scrim,
.drawer-leave-active .toc__scrim {
  transition: opacity 0.22s ease;
}
.drawer-enter-from .toc__scrim,
.drawer-leave-to .toc__scrim {
  opacity: 0;
}
</style>
