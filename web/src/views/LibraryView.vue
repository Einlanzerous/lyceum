<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import BookCard from '@/components/BookCard.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { useLibraryStore } from '@/stores/library'
import { coverUrl } from '@/api/client'
import { formatProgress } from '@/api/progress'

type Toast = { kind: 'success' | 'error'; title: string; subtitle: string }

const store = useLibraryStore()
const { books, loading, error } = storeToRefs(store)

const fileInput = ref<HTMLInputElement | null>(null)
const dragging = ref(false)
const view = ref<'grid' | 'list'>('grid')
const toast = ref<Toast | null>(null)
let toastTimer: ReturnType<typeof setTimeout> | undefined
let dragDepth = 0

const count = computed(() => books.value.length)
const countLabel = computed(() => `${count.value} on the shelf`)

onMounted(() => store.load())

function showToast(t: Toast): void {
  toast.value = t
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toast.value = null), 4000)
}

function isEpub(f: File): boolean {
  return f.type === 'application/epub+zip' || f.name.toLowerCase().endsWith('.epub')
}

/** Upload the EPUBs and summarise the outcome — including non-EPUBs skipped. */
async function ingest(all: File[]): Promise<void> {
  if (all.length === 0) return
  const epubs = all.filter(isEpub)
  const skipped = all.filter((f) => !isEpub(f))

  const results = epubs.length ? await store.uploadMany(epubs) : []
  const added = results.filter((r) => r.kind === 'added').length
  const duplicates = results.filter((r) => r.kind === 'duplicate').length
  const failed = results.filter((r) => r.kind === 'error').length

  if (added > 0) {
    const extra: string[] = []
    if (duplicates) extra.push(`${duplicates} already on the shelf`)
    if (skipped.length) extra.push(`${skipped.length} not an EPUB`)
    if (failed) extra.push(`${failed} failed`)
    showToast({
      kind: 'success',
      title: `${added} added to your library`,
      subtitle: extra.length ? extra.join(' · ') + ' · skipped' : 'Your shelf just grew',
    })
    return
  }

  // Nothing added — explain why.
  if (failed > 0) {
    showToast({ kind: 'error', title: 'Upload failed', subtitle: 'The server rejected the file' })
  } else if (skipped.length > 0) {
    const title =
      skipped.length === 1 ? `"${skipped[0]!.name}" isn't an EPUB` : `${skipped.length} files aren't EPUBs`
    showToast({ kind: 'error', title, subtitle: 'Nothing was added' })
  } else if (duplicates > 0) {
    showToast({ kind: 'success', title: 'Already on your shelf', subtitle: 'Nothing new to add' })
  }
}

function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  void ingest(Array.from(input.files ?? []))
  input.value = ''
}

function onDrop(event: DragEvent): void {
  dragDepth = 0
  dragging.value = false
  void ingest(Array.from(event.dataTransfer?.files ?? []))
}

// Depth-counted so child elements don't flicker the overlay off on dragleave.
function onDragEnter(): void {
  dragDepth++
  dragging.value = true
}
function onDragLeave(): void {
  dragDepth = Math.max(0, dragDepth - 1)
  if (dragDepth === 0) dragging.value = false
}
</script>

<template>
  <section
    class="lib"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent
    @dragleave.prevent="onDragLeave"
    @drop.prevent="onDrop"
  >
    <!-- Floating top bar -->
    <header class="lib__bar">
      <div class="lib__brand">
        <span class="lib__brand-mark" aria-hidden="true" />
        <span class="lib__brand-name">LYCEUM</span>
      </div>

      <div class="lib__actions">
        <div class="lib__toggle" role="group" aria-label="View">
          <button
            type="button"
            class="lib__toggle-btn"
            :class="{ 'is-active': view === 'grid' }"
            aria-label="Grid view"
            @click="view = 'grid'"
          >
            ▦
          </button>
          <button
            type="button"
            class="lib__toggle-btn"
            :class="{ 'is-active': view === 'list' }"
            aria-label="List view"
            @click="view = 'list'"
          >
            ☰
          </button>
        </div>
        <ThemeToggle />
        <button type="button" class="lib__add" @click="fileInput?.click()">
          <span class="lib__add-plus">+</span> Add EPUB
        </button>
        <RouterLink to="/settings" class="lib__avatar" aria-label="Settings" title="Settings">R</RouterLink>
        <input
          ref="fileInput"
          type="file"
          accept="application/epub+zip,.epub"
          multiple
          hidden
          @change="onPick"
        />
      </div>
    </header>

    <!-- Header -->
    <div class="lib__head">
      <div class="lib__eyebrow">Your library</div>
      <div class="lib__title-row">
        <h1 class="lib__title">All Books</h1>
        <span v-if="!loading && !error" class="lib__count">{{ countLabel }}</span>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="lib__state">
      <div class="lib__skeletons">
        <div v-for="n in 6" :key="n" class="lib__skeleton" :style="{ animationDelay: n * 0.12 + 's' }" />
      </div>
      <div class="lib__loading-label"><span class="lib__dot" /> Reading your shelf…</div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="lib__state lib__state--center lib__state--error" role="alert">
      <div class="lib__icon lib__icon--error">!</div>
      <div class="lib__state-title">Can't reach the library</div>
      <p class="lib__state-text">{{ error }}</p>
      <button type="button" class="btn btn--ghost" @click="store.load()">↻ Try again</button>
    </div>

    <!-- Empty -->
    <div v-else-if="count === 0" class="lib__state lib__state--center">
      <div class="lib__empty-icon"><span>+</span></div>
      <div class="lib__state-title">No books yet</div>
      <p class="lib__state-text">Drop an EPUB anywhere, or use Add EPUB to begin your shelf.</p>
      <button type="button" class="btn btn--brass" @click="fileInput?.click()">+ Add EPUB</button>
    </div>

    <!-- Grid -->
    <div v-else-if="view === 'grid'" class="lib__grid">
      <BookCard v-for="book in books" :key="book.id" :book="book" />
    </div>

    <!-- List -->
    <div v-else class="lib__list">
      <RouterLink v-for="book in books" :key="book.id" :to="`/reader/${book.id}`" class="row">
        <div class="row__thumb" :class="{ 'row__thumb--fallback': !book.cover_url }">
          <img v-if="book.cover_url" :src="coverUrl(book.id)" :alt="''" loading="lazy" />
          <span v-else>{{ book.title.charAt(0) }}</span>
        </div>
        <div class="row__meta">
          <div class="row__title">{{ book.title }}</div>
          <div class="row__author">{{ book.author }}</div>
        </div>
        <div v-if="typeof book.progress === 'number'" class="row__progress">
          {{ formatProgress(book.progress) }}
        </div>
      </RouterLink>
    </div>

    <!-- Full-page drag-over affordance -->
    <div v-if="dragging" class="drop" aria-hidden="true">
      <div class="drop__panel">
        <div class="drop__icon" />
        <div class="drop__title">Drop to add to your library</div>
        <div class="drop__sub">EPUB files only · release anywhere on the page</div>
      </div>
    </div>

    <!-- Toast -->
    <Transition name="toast">
      <output v-if="toast" class="toast" :class="`toast--${toast.kind}`">
        <span class="toast__icon">{{ toast.kind === 'success' ? '✓' : '!' }}</span>
        <span class="toast__body">
          <span class="toast__title">{{ toast.title }}</span>
          <span class="toast__sub">{{ toast.subtitle }}</span>
        </span>
      </output>
    </Transition>
  </section>
</template>

<style scoped>
.lib {
  position: relative;
  min-height: 100%;
  padding: 0 clamp(20px, 4vw, 36px) 60px;
}

/* ── Top bar ── */
.lib__bar {
  position: sticky;
  top: 0;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 20px 0;
  background: linear-gradient(var(--bg) 30%, transparent);
}
.lib__brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 18px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(10px);
}
.lib__brand-mark {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  border: 2px solid var(--brass);
  transform: rotate(45deg);
}
.lib__brand-name {
  font: 800 15px var(--font-display);
  letter-spacing: 0.16em;
  color: var(--text);
}
.lib__actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 10px;
}
.lib__toggle {
  display: flex;
  align-items: center;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  overflow: hidden;
}
.lib__toggle-btn {
  padding: 8px 13px;
  border: none;
  background: transparent;
  color: var(--dim);
  font: 700 13px var(--font-ui);
  cursor: pointer;
}
.lib__toggle-btn.is-active {
  background: var(--brass);
  color: var(--on-brass);
}
.lib__add {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 10px 18px;
  border-radius: 999px;
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font: 700 13.5px var(--font-ui);
  cursor: pointer;
  transition: background 0.15s ease;
}
.lib__add:hover {
  background: var(--brass-bright);
}
.lib__add-plus {
  font-size: 16px;
  line-height: 1;
}
.lib__avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: 1px solid var(--border-strong);
  background: linear-gradient(135deg, var(--brass), #7a5d2c);
  display: flex;
  align-items: center;
  justify-content: center;
  font: 700 14px var(--font-display);
  color: var(--on-brass);
  text-decoration: none;
}

/* ── Header ── */
.lib__head {
  margin: 8px 0 26px;
}
.lib__eyebrow {
  font: 700 12px var(--font-display);
  letter-spacing: 0.2em;
  color: var(--brass);
  text-transform: uppercase;
}
.lib__title-row {
  display: flex;
  align-items: baseline;
  gap: 16px;
  margin-top: 8px;
}
.lib__title {
  margin: 0;
  font: 800 clamp(28px, 4vw, 36px) var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
}
.lib__count {
  font: 400 14px var(--font-ui);
  color: var(--dim);
}

/* ── Grid ── */
.lib__grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 26px 22px;
}
@media (max-width: 1200px) {
  .lib__grid {
    grid-template-columns: repeat(4, 1fr);
  }
}
@media (max-width: 760px) {
  .lib__grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

/* ── List ── */
.lib__list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 720px;
}
.row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--surface);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.15s ease;
}
.row:hover {
  border-color: var(--border-strong);
}
.row__thumb {
  width: 38px;
  height: 56px;
  flex: none;
  border-radius: 4px;
  overflow: hidden;
  background: var(--surface-raised);
  display: flex;
  align-items: center;
  justify-content: center;
}
.row__thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.row__thumb--fallback span {
  font: 800 18px var(--font-display);
  color: var(--brass);
}
.row__meta {
  min-width: 0;
  flex: 1;
}
.row__title {
  font: 700 14px var(--font-ui);
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row__author {
  font: 400 12px var(--font-ui);
  color: var(--dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row__progress {
  font: 700 12px var(--font-ui);
  color: var(--brass-bright);
}

/* ── States ── */
.lib__state {
  border-radius: 11px;
}
.lib__state--center {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  min-height: 320px;
  padding: 40px 30px;
  background: var(--surface);
  border: 1px solid var(--border);
}
.lib__state--error {
  border-color: rgba(224, 138, 110, 0.22);
}
.lib__state-title {
  font: 800 19px var(--font-display);
  color: var(--text);
}
.lib__state-text {
  font: 400 13px/1.5 var(--font-ui);
  color: var(--muted);
  margin: 7px 0 0;
  max-width: 360px;
}
.lib__icon {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font: 400 24px var(--font-ui);
  margin-bottom: 18px;
}
.lib__icon--error {
  border: 1.5px solid rgba(224, 138, 110, 0.5);
  color: var(--error);
}
.lib__empty-icon {
  width: 52px;
  height: 62px;
  border-radius: 5px;
  border: 1.5px dashed rgba(201, 154, 78, 0.5);
  position: relative;
  margin-bottom: 20px;
}
.lib__empty-icon span {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  font: 300 30px var(--font-ui);
  color: var(--brass);
  line-height: 1;
}
.btn {
  margin-top: 18px;
  padding: 9px 18px;
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

/* Loading skeletons */
.lib__skeletons {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 22px;
}
@media (max-width: 1200px) {
  .lib__skeletons {
    grid-template-columns: repeat(4, 1fr);
  }
}
@media (max-width: 760px) {
  .lib__skeletons {
    grid-template-columns: repeat(3, 1fr);
  }
}
.lib__skeleton {
  aspect-ratio: 2 / 3;
  border-radius: 7px;
  background: linear-gradient(100deg, var(--surface) 30%, var(--surface-raised) 50%, var(--surface) 70%);
  background-size: 200% 100%;
  animation: lycShimmer 1.4s linear infinite;
}
.lib__loading-label {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  margin-top: 24px;
  font: 500 12px var(--font-ui);
  color: var(--muted);
}
.lib__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--brass);
  animation: lycPulse 1.1s ease-in-out infinite;
}

/* ── Drag overlay ── */
.drop {
  position: fixed;
  inset: 14px;
  z-index: 30;
  border-radius: 14px;
  pointer-events: none;
}
.drop__panel {
  position: absolute;
  inset: 0;
  border-radius: 14px;
  border: 2px dashed rgba(201, 154, 78, 0.7);
  background: color-mix(in srgb, var(--bg) 72%, transparent);
  backdrop-filter: blur(3px);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
}
.drop__icon {
  width: 54px;
  height: 64px;
  border-radius: 6px;
  border: 2px solid var(--brass);
  margin-bottom: 18px;
}
.drop__title {
  font: 800 24px var(--font-display);
  color: var(--text);
}
.drop__sub {
  font: 400 13.5px var(--font-ui);
  color: var(--brass);
  margin-top: 8px;
}

/* ── Toast ── */
.toast {
  position: fixed;
  left: 50%;
  bottom: 24px;
  transform: translateX(-50%);
  z-index: 40;
  display: flex;
  align-items: center;
  gap: 13px;
  padding: 14px 16px;
  border-radius: 12px;
  background: var(--surface-raised);
  box-shadow: var(--shadow-pop);
  max-width: 92vw;
}
.toast--success {
  border: 1px solid rgba(201, 154, 78, 0.28);
}
.toast--error {
  border: 1px solid rgba(224, 138, 110, 0.28);
}
.toast__icon {
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font: 700 15px var(--font-ui);
}
.toast--success .toast__icon {
  background: rgba(90, 168, 106, 0.16);
  color: var(--success);
}
.toast--error .toast__icon {
  background: rgba(224, 138, 110, 0.16);
  color: var(--error);
}
.toast__body {
  display: flex;
  flex-direction: column;
}
.toast__title {
  font: 700 13.5px var(--font-ui);
  color: var(--text);
}
.toast__sub {
  font: 400 12px var(--font-ui);
  color: var(--muted);
  margin-top: 1px;
}
.toast-enter-active {
  animation: lycRise 0.2s ease both;
}
.toast-leave-active {
  transition: opacity 0.2s ease;
}
.toast-leave-to {
  opacity: 0;
}
</style>
