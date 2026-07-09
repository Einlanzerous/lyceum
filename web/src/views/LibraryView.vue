<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import BookCard from '@/components/BookCard.vue'
import SeriesCard from '@/components/SeriesCard.vue'
import SeriesDrawer from '@/components/SeriesDrawer.vue'
import SortControl from '@/components/SortControl.vue'
import LibrarySearch from '@/components/LibrarySearch.vue'
import { useLibraryStore } from '@/stores/library'
import { coverUrl } from '@/api/client'
import { formatProgress } from '@/api/progress'
import { isNativeShell } from '@/api/base'
import { useServer } from '@/api/useServer'
import { useProfile } from '@/profile'
import ServerSettings from '@/components/ServerSettings.vue'
import { loadSort, saveSort, sortBooks, type SortDir, type SortKey } from '@/library/sort'
import { buildShelf, pinnedBookId } from '@/library/series'
import { useGridColumns } from '@/library/useColumns'
import type { Book } from '@/api/types'

const store = useLibraryStore()
const { initial } = useProfile()
const { books, loading, error } = storeToRefs(store)

// In the native shells the library can't load until a backend is configured
// (LYCM-300). Show a connect prompt instead of a failed fetch on first run.
const { server } = useServer()
const needsServer = computed(() => isNativeShell() && !server.value)

const view = ref<'grid' | 'list'>('grid')

// Sort order (LYCM-62), remembered across sessions.
const sort = ref(loadSort())
watch(sort, (s) => saveSort(s), { deep: true })
function setSortKey(key: SortKey): void {
  sort.value = { ...sort.value, key }
}
function setSortDir(dir: SortDir): void {
  sort.value = { ...sort.value, dir }
}

// Search overlay (LYCM-63). The shelf filters live underneath as you type.
const searchOpen = ref(false)
const query = ref('')
const searching = computed(() => query.value.trim().length > 0)

function openSearch(): void {
  if (!hasShelf.value) return
  searchOpen.value = true
}
function closeSearch(): void {
  searchOpen.value = false
  query.value = '' // closing restores the full shelf
}

const matchedBooks = computed<Book[]>(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return []
  const hit = books.value.filter(
    (b) =>
      b.title.toLowerCase().includes(q) ||
      b.author.toLowerCase().includes(q) ||
      (b.series ?? '').toLowerCase().includes(q),
  )
  return sortBooks(hit, sort.value)
})

// The most-recently-read book is pinned to the top of the shelf (its series card
// floats up if it belongs to one), so "continue reading" is always first.
const pinnedId = computed(() => pinnedBookId(books.value))

// Series roll-up (LYCM-36): group + sort the shelf, and manage the inline drawer.
const shelfItems = computed(() => buildShelf(books.value, sort.value, pinnedId.value))
const listBooks = computed(() => {
  const sorted = sortBooks(books.value, sort.value)
  const at = pinnedId.value == null ? -1 : sorted.findIndex((b) => b.id === pinnedId.value)
  if (at > 0) sorted.unshift(sorted.splice(at, 1)[0]!)
  return sorted
})

const openKey = ref<string | null>(null)
const cols = useGridColumns()

function toggleSeries(key: string): void {
  openKey.value = openKey.value === key ? null : key
}

const openIndex = computed(() =>
  openKey.value ? shelfItems.value.findIndex((i) => i.key === openKey.value) : -1,
)
const openSeries = computed(() => {
  const item = openIndex.value >= 0 ? shelfItems.value[openIndex.value] : undefined
  return item && item.kind === 'series' ? item.series : null
})
// The drawer slots in at the end of the row holding the open card, so the row
// below reflows down (Option A).
const drawerAfterIndex = computed(() => {
  const i = openIndex.value
  if (i < 0) return -1
  const c = cols.value
  return Math.min(Math.floor(i / c) * c + c - 1, shelfItems.value.length - 1)
})
const arrowLeftPct = computed(() => {
  const c = cols.value
  return (((openIndex.value % c) + 0.5) / c) * 100
})

// A changed sort or an active search invalidates the open drawer's position.
watch([sort, query, searchOpen], () => {
  openKey.value = null
})

const count = computed(() => books.value.length)
const countLabel = computed(() =>
  searching.value
    ? `${matchedBooks.value.length} of ${count.value}`
    : `${count.value} on the shelf`,
)

// Controls (sort, search, view toggle) only make sense once a shelf is showing.
const hasShelf = computed(
  () => !needsServer.value && !loading.value && !error.value && count.value > 0,
)

function onKeydown(e: KeyboardEvent): void {
  if (searchOpen.value) return // the overlay owns Escape while open
  if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
    e.preventDefault()
    openSearch()
    return
  }
  if (e.key === '/' && !isTypingTarget(e.target)) {
    e.preventDefault()
    openSearch()
  }
}
function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

onMounted(() => {
  if (!needsServer.value) store.load()
  window.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

// Once the user saves a server, load the shelf from it.
function onServerSaved(): void {
  void store.load()
}

function onSetFinished(id: number, finished: boolean): void {
  void store.setFinished(id, finished)
}
</script>

<template>
  <section class="lib">
    <!-- Floating top bar -->
    <header class="lib__bar">
      <div class="lib__brand">
        <span class="lib__brand-mark" aria-hidden="true" />
        <span class="lib__brand-name">LYCEUM</span>
      </div>

      <div class="lib__actions">
        <RouterLink to="/settings" class="lib__avatar" aria-label="Settings" title="Settings">{{
          initial
        }}</RouterLink>
      </div>
    </header>

    <!-- Header -->
    <div class="lib__head">
      <div class="lib__eyebrow">Your library</div>
      <div class="lib__title-row">
        <div class="lib__title-group">
          <h1 class="lib__title">All Books</h1>
          <span v-if="!loading && !error" class="lib__count">{{ countLabel }}</span>
        </div>
        <div v-if="hasShelf" class="lib__controls">
          <SortControl
            :sort-key="sort.key"
            :dir="sort.dir"
            @update:sort-key="setSortKey"
            @update:dir="setSortDir"
          />
          <button
            type="button"
            class="lib__search-btn"
            aria-label="Search library"
            title="Search (press /)"
            @click="openSearch"
          >
            ⌕
          </button>
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
        </div>
      </div>
    </div>

    <LibrarySearch
      v-model="query"
      :open="searchOpen"
      :result-count="matchedBooks.length"
      @close="closeSearch"
    />

    <!-- Connect prompt (native shells, first run) -->
    <div v-if="needsServer" class="lib__state lib__state--center">
      <div class="lib__empty-icon"><span>⇄</span></div>
      <div class="lib__state-title">Connect to your library</div>
      <p class="lib__state-text">
        Enter the address of your Lyceum server to load your books and sync your place.
      </p>
      <div class="lib__connect">
        <ServerSettings @saved="onServerSaved" />
      </div>
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="lib__state">
      <div class="lib__skeletons">
        <div
          v-for="n in 6"
          :key="n"
          class="lib__skeleton"
          :style="{ animationDelay: n * 0.12 + 's' }"
        />
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
      <p class="lib__state-text">Books appear here once your server ingests them.</p>
    </div>

    <!-- No search matches -->
    <div v-else-if="searching && matchedBooks.length === 0" class="lib__state lib__state--center">
      <div class="lib__empty-icon"><span>⌕</span></div>
      <div class="lib__state-title">No matches</div>
      <p class="lib__state-text">Nothing on the shelf matches “{{ query.trim() }}”.</p>
    </div>

    <!-- Search results (flat, ungrouped) -->
    <div v-else-if="searching" class="lib__grid">
      <BookCard
        v-for="book in matchedBooks"
        :key="book.id"
        :book="book"
        @set-finished="onSetFinished"
      />
    </div>

    <!-- Grid — series roll up into cards; an open series expands inline. -->
    <div v-else-if="view === 'grid'" class="lib__grid">
      <template v-for="(item, i) in shelfItems" :key="item.key">
        <BookCard
          v-if="item.kind === 'book'"
          :book="item.book"
          :pinned="pinnedId != null && item.book.id === pinnedId"
          @set-finished="onSetFinished"
        />
        <SeriesCard
          v-else
          :series="item.series"
          :open="openKey === item.key"
          :continue-book-id="
            pinnedId != null && item.series.members.some((m) => m.id === pinnedId) ? pinnedId : null
          "
          @toggle="toggleSeries(item.key)"
        />
        <Transition name="drawer">
          <SeriesDrawer
            v-if="openSeries && i === drawerAfterIndex"
            :series="openSeries"
            :arrow-left-pct="arrowLeftPct"
            @close="openKey = null"
          />
        </Transition>
      </template>
    </div>

    <!-- List -->
    <div v-else class="lib__list">
      <RouterLink v-for="book in listBooks" :key="book.id" :to="`/reader/${book.id}`" class="row">
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
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 8px;
  flex-wrap: wrap;
}
.lib__title-group {
  display: flex;
  align-items: baseline;
  gap: 16px;
  min-width: 0;
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

/* Sort + search + view toggle cluster, across from the title. */
.lib__controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.lib__search-btn {
  width: 36px;
  height: 36px;
  flex: none;
  border-radius: 50%;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  color: var(--text);
  font-size: 17px;
  cursor: pointer;
}
.lib__search-btn:hover {
  border-color: var(--brass);
  color: var(--brass);
}

/* View toggle — lives across from the title now to keep the top bar clean. */
.lib__toggle {
  display: flex;
  align-items: center;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  overflow: hidden;
  flex: none;
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
.lib__connect {
  width: 100%;
  max-width: 440px;
  margin-top: 22px;
  text-align: left;
}
.btn {
  margin-top: 18px;
  padding: 9px 18px;
  border-radius: 999px;
  font: 700 12.5px var(--font-ui);
  cursor: pointer;
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
  aspect-ratio: 366 / 600;
  border-radius: 7px;
  background: linear-gradient(
    100deg,
    var(--surface) 30%,
    var(--surface-raised) 50%,
    var(--surface) 70%
  );
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
</style>
