<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useIngestStore, type QueueFilter } from '@/stores/ingest'
import type { Candidate, Edition } from '@/api/ingest'
import { useAuthStore } from '@/stores/auth'

const store = useIngestStore()
const auth = useAuthStore()
const initial = computed(() => auth.initial)
const {
  batch,
  batches,
  visible,
  selected,
  counts,
  reviewable,
  filter,
  search,
  busy,
  loading,
  error,
} = storeToRefs(store)

// Series assignment inputs for the selected candidate.
const seriesName = ref('')
const seriesIndex = ref<number | null>(null)
// Sticky series carry-over (LYCM-75): a run of books in one series keeps the
// series name and an auto-incrementing index across confirms, instead of the
// selection resetting them each time.
let stickySeries = ''
let stickyIndex: number | null = null

// Add-book modal: a single combined field (title search or ISBN) reached from
// the "＋" button on the batch, replacing the always-on add bar (LYCM-75).
const showAdd = ref(false)
const addQuery = ref('')
// Inline re-resolve field on a no-match card.
const reIsbn = ref('')

onMounted(() => {
  void store.loadBatches()
})

watch(selected, (c) => {
  reIsbn.value = ''
  if (c?.series) {
    // A candidate with its own saved series wins; show its values.
    seriesName.value = c.series
    seriesIndex.value = c.series_index ?? null
  } else {
    // Otherwise inherit the sticky series carried over from the last confirm so
    // consecutive books in a series don't lose the assignment.
    seriesName.value = stickySeries
    seriesIndex.value = stickyIndex
  }
})

const scannedCount = computed(() => store.candidates.length)

const chosen = computed<Edition | null>(() => {
  const c = selected.value
  if (!c) return null
  if (c.chosen_edition_id) {
    return c.editions.find((e) => e.id === c.chosen_edition_id) ?? c.editions[0] ?? null
  }
  return c.editions[0] ?? null
})

const filters = computed<{ key: QueueFilter; label: string; n: number }[]>(() => [
  { key: 'all', label: 'All', n: reviewable.value.length },
  { key: 'ready', label: 'Ready', n: counts.value.ready },
  { key: 'review', label: 'Review', n: counts.value.review },
  { key: 'no_match', label: 'No match', n: counts.value.no_match },
])

function statusLabel(s: Candidate['status']): string {
  return (
    (
      { ready: 'Ready', review: 'Review', no_match: 'No match', duplicate: 'Duplicate' } as Record<
        string,
        string
      >
    )[s] ?? s
  )
}

function pct(c: Candidate): string {
  return c.confidence > 0 ? `${Math.round(c.confidence * 100)}%` : ''
}

function subtitle(c: Candidate): string {
  if (c.status === 'review') return `${c.editions.length} editions matched`
  if (c.status === 'no_match') return c.isbn || 'No DRM edition'
  return c.author ?? ''
}

async function onConfirm(): Promise<void> {
  const name = seriesName.value.trim()
  const idx = seriesIndex.value
  // Carry the series onto the next book before confirm() advances the selection
  // (which re-fills the inputs from the sticky state); auto-increment the index
  // so a series is numbered as you go.
  stickySeries = name
  stickyIndex = name && idx != null ? idx + 1 : idx
  await store.confirm(name, idx ?? 0)
}
async function onPick(editionId: string): Promise<void> {
  await store.pick(editionId)
}
async function onConfirmAll(): Promise<void> {
  await store.confirmAllReady()
}
async function onReResolve(): Promise<void> {
  const c = selected.value
  if (!c) return
  await store.reResolve(c.id, reIsbn.value)
}

// --- Add-book modal: one field, title-search or ISBN ---

/**
 * True when the input is unambiguously an ISBN (10 or 13 digits, ignoring
 * hyphens/spaces, a trailing X allowed) rather than a title to search. Title
 * and ISBN inputs don't collide in practice, so one field can serve both.
 */
function looksLikeIsbn(q: string): boolean {
  const s = q.replace(/[\s-]/g, '')
  return /^\d{9}[\dxX]$/.test(s) || /^\d{13}$/.test(s)
}

function openAdd(): void {
  addQuery.value = ''
  store.clearSearch()
  showAdd.value = true
}
function closeAdd(): void {
  showAdd.value = false
  addQuery.value = ''
  store.clearSearch()
}

let searchTimer: ReturnType<typeof setTimeout> | undefined
function onAddInput(): void {
  clearTimeout(searchTimer)
  const q = addQuery.value
  if (looksLikeIsbn(q)) {
    // An ISBN — skip the title search; Enter (or a nudge) adds it directly.
    store.clearSearch()
    return
  }
  searchTimer = setTimeout(() => void store.runSearch(q), 250)
}
async function onAddSubmit(): Promise<void> {
  const q = addQuery.value.trim()
  if (!looksLikeIsbn(q)) return
  await store.addByIsbn(q, 'manual')
  closeAdd()
}
async function onPickResult(edition: Edition): Promise<void> {
  const code = edition.isbn13 || edition.id
  await store.addByIsbn(code, 'title')
  closeAdd()
}

function relTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const mins = Math.round((Date.now() - then) / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins} min ago`
  const hrs = Math.round(mins / 60)
  if (hrs < 24) return `${hrs} h ago`
  return `${Math.round(hrs / 24)} d ago`
}
</script>

<template>
  <section class="ing">
    <!-- Top bar -->
    <header class="ing__bar">
      <RouterLink to="/" class="ing__brand" title="Back to library">
        <span class="ing__mark" aria-hidden="true" />
        <span class="ing__name">LYCEUM</span>
      </RouterLink>
      <span class="ing__crumb-sep">/</span>
      <span class="ing__crumb">Ingest</span>
      <span class="ing__crumb-sep">/</span>
      <span class="ing__crumb ing__crumb--active">Verify batch</span>
      <div class="ing__bar-right">
        <span v-if="batch" class="ing__uploaded">
          Uploaded from {{ batch.source_device || 'device' }} · {{ relTime(batch.created_at) }}
        </span>
        <span class="ing__avatar">{{ initial }}</span>
      </div>
    </header>

    <p v-if="error" class="ing__error">{{ error }}</p>

    <!-- No active batch: batch picker -->
    <div v-if="!batch" class="ing__pick">
      <div class="ing__pick-head">
        <div>
          <div class="ing__eyebrow">ISBN ingest</div>
          <h1 class="ing__h1">Verify scanned books</h1>
          <p class="ing__lead">
            Batch-scan barcodes on mobile, then confirm the matched titles here before they join
            your library. No batch open — start one and add books by title or ISBN.
          </p>
        </div>
        <button class="btn btn--brass" :disabled="busy" @click="store.newBatch()">
          ＋ New batch
        </button>
      </div>

      <div v-if="loading" class="ing__muted">Loading batches…</div>
      <ul v-else-if="batches.length" class="ing__batches">
        <li v-for="b in batches" :key="b.id">
          <button class="ing__batch" @click="store.openBatch(b.id)">
            <span class="ing__batch-title">Batch #{{ b.id }}</span>
            <span class="ing__batch-meta">
              {{ b.source_device || 'device' }} · {{ relTime(b.created_at) }} ·
              {{ Object.values(b.counts).reduce((a, n) => a + n, 0) }} scanned
            </span>
            <span class="ing__batch-status" :class="`is-${b.status}`">{{ b.status }}</span>
          </button>
        </li>
      </ul>
      <div v-else class="ing__muted">No batches yet.</div>
    </div>

    <!-- Active batch: verify -->
    <template v-else>
      <div class="ing__head">
        <div>
          <div class="ing__eyebrow">Review before shelving</div>
          <div class="ing__title-row">
            <h1 class="ing__h1">{{ scannedCount }} scanned books</h1>
            <span class="ing__submeta">
              {{ counts.ready }} ready · {{ counts.review }} needs review · {{ counts.no_match }} no
              match
            </span>
          </div>
        </div>
        <div class="ing__head-actions">
          <button class="btn btn--ghost" @click="store.$patch({ batch: null })">Close</button>
          <button
            class="btn btn--brass"
            :disabled="busy || counts.ready === 0"
            @click="onConfirmAll"
          >
            Confirm {{ counts.ready }} &amp; add to library
          </button>
        </div>
      </div>

      <div class="ing__split">
        <!-- Queue -->
        <div class="ing__queue">
          <div class="ing__queue-head">
            <div class="ing__chips">
              <button
                v-for="f in filters"
                :key="f.key"
                class="chip"
                :class="{ 'chip--on': filter === f.key, [`chip--${f.key}`]: true }"
                @click="store.setFilter(f.key)"
              >
                {{ f.label }} {{ f.n }}
              </button>
            </div>
            <button
              class="ing__add-btn"
              title="Add a book to this batch"
              aria-label="Add a book to this batch"
              @click="openAdd"
            >
              ＋
            </button>
          </div>
          <div class="ing__list">
            <button
              v-for="c in visible"
              :key="c.id"
              class="row"
              :class="{ 'row--sel': selected && selected.id === c.id, [`row--${c.status}`]: true }"
              @click="store.select(c.id)"
            >
              <span class="row__cover">
                <img v-if="c.cover_url" :src="c.cover_url" alt="" />
                <span v-else class="row__cover-fallback">{{
                  (c.title || c.isbn).slice(0, 3)
                }}</span>
              </span>
              <span class="row__main">
                <span class="row__title">{{ c.title || 'No DRM edition' }}</span>
                <span class="row__sub" :class="{ 'row__sub--mono': c.status === 'no_match' }">{{
                  subtitle(c)
                }}</span>
              </span>
              <span class="row__status">
                <span class="dot" :class="`dot--${c.status}`" />
                <span class="row__status-label" :class="`is-${c.status}`">{{
                  statusLabel(c.status)
                }}</span>
                <span v-if="pct(c)" class="row__pct">{{ pct(c) }}</span>
              </span>
            </button>
            <p v-if="!visible.length" class="ing__muted ing__muted--pad">
              All clear — nothing to review.
            </p>
          </div>
        </div>

        <!-- Detail -->
        <div class="ing__detail">
          <div v-if="!selected" class="ing__empty">Select a book to review.</div>

          <template v-else>
            <div class="detail__top">
              <div class="detail__cover-wrap">
                <div class="detail__cover">
                  <img
                    v-if="chosen?.cover_url || selected.cover_url"
                    :src="chosen?.cover_url || selected.cover_url"
                    alt=""
                  />
                  <div v-else class="detail__cover-fallback">
                    <span>{{ selected.title || selected.isbn }}</span>
                  </div>
                </div>
              </div>

              <div class="detail__meta">
                <span class="badge" :class="`badge--${selected.status}`">
                  <span class="dot" :class="`dot--${selected.status}`" />
                  <template v-if="selected.status === 'ready'"
                    >High-confidence match · {{ pct(selected) }}</template
                  >
                  <template v-else-if="selected.status === 'review'"
                    >{{ selected.editions.length }} editions · pick one</template
                  >
                  <template v-else-if="selected.status === 'duplicate'"
                    >Already in your library</template
                  >
                  <template v-else>No DRM edition found</template>
                </span>

                <h2 class="detail__title">{{ selected.title || 'Unknown title' }}</h2>
                <div class="detail__author">{{ selected.author || chosen?.author || '—' }}</div>

                <div v-if="selected.isbn" class="detail__trace">
                  <span class="mono">scanned {{ selected.isbn }}</span>
                  <span v-if="selected.status !== 'no_match'" class="detail__trace-arrow">→</span>
                  <span v-if="selected.status !== 'no_match'" class="detail__trace-to"
                    >matched edition</span
                  >
                </div>

                <div v-if="chosen" class="detail__fields">
                  <div>
                    <dt>ISBN-13</dt>
                    <dd class="mono">{{ chosen.isbn13 || selected.isbn }}</dd>
                  </div>
                  <div>
                    <dt>Publisher</dt>
                    <dd>{{ chosen.publisher || '—' }}</dd>
                  </div>
                  <div>
                    <dt>Published</dt>
                    <dd>{{ chosen.year || '—' }}</dd>
                  </div>
                  <div>
                    <dt>Pages</dt>
                    <dd>{{ chosen.pages || '—' }}</dd>
                  </div>
                  <div>
                    <dt>Language</dt>
                    <dd>{{ chosen.language || '—' }}</dd>
                  </div>
                  <div>
                    <dt>Format</dt>
                    <dd>EPUB</dd>
                  </div>
                </div>

                <!-- Series assignment (reuses the Series feature's fields) -->
                <div class="detail__series">
                  <dt>Series</dt>
                  <div class="detail__series-inputs">
                    <input
                      v-model="seriesName"
                      class="ing__input"
                      type="text"
                      placeholder="Assign to a series (optional)"
                    />
                    <input
                      v-model.number="seriesIndex"
                      class="ing__input ing__input--num"
                      type="number"
                      min="1"
                      step="1"
                      placeholder="#"
                    />
                  </div>
                </div>
              </div>
            </div>

            <!-- Flagged review: edition picker -->
            <div v-if="selected.status === 'review'" class="picker">
              <div class="picker__label">
                <span class="dot dot--review" /> Pick the intended edition
              </div>
              <div class="picker__grid">
                <button
                  v-for="e in selected.editions"
                  :key="e.id"
                  class="edition"
                  :class="{ 'edition--on': selected.chosen_edition_id === e.id }"
                  :disabled="busy"
                  @click="onPick(e.id)"
                >
                  <div class="edition__cover">
                    <img v-if="e.cover_url" :src="e.cover_url" alt="" />
                    <span v-else>{{ e.title.slice(0, 4) }}</span>
                  </div>
                  <div class="edition__title">
                    {{ e.publisher || e.title }}{{ e.year ? ` · ${e.year}` : '' }}
                  </div>
                  <div class="edition__meta mono">{{ e.isbn13 || e.id }}</div>
                </button>
              </div>
            </div>

            <!-- No match: re-resolve inline with a corrected ISBN -->
            <div v-else-if="selected.status === 'no_match'" class="fallback">
              <p class="fallback__lead">
                No DRM edition resolved for this scan. If the scanned code was wrong, re-resolve it
                with a corrected ISBN — or skip it and upload the EPUB directly from the library.
              </p>
              <form class="fallback__form" @submit.prevent="onReResolve">
                <input
                  v-model="reIsbn"
                  class="ing__input"
                  type="text"
                  inputmode="numeric"
                  placeholder="Corrected ISBN"
                />
                <button class="btn btn--brass" type="submit" :disabled="busy || !reIsbn.trim()">
                  Re-resolve
                </button>
              </form>
            </div>

            <!-- Action bar -->
            <div class="detail__actions">
              <button
                class="btn btn--brass"
                :disabled="busy || selected.status === 'no_match' || selected.status === 'review'"
                @click="onConfirm"
              >
                ✓ Confirm this book
              </button>
              <div class="detail__spacer" />
              <button class="btn btn--danger" :disabled="busy" @click="store.skip()">
                Skip / remove
              </button>
            </div>
          </template>
        </div>
      </div>

      <!-- Add-book modal: single combined field, back-arrow nav (LYCM-75) -->
      <div
        v-if="showAdd"
        class="addm__scrim"
        role="dialog"
        aria-modal="true"
        @click.self="closeAdd"
      >
        <div class="addm">
          <div class="addm__head">
            <button class="addm__back" title="Back" aria-label="Back" @click="closeAdd">←</button>
            <h2 class="addm__title">Add a book to this batch</h2>
          </div>
          <form class="addm__search" @submit.prevent="onAddSubmit">
            <input
              v-model="addQuery"
              class="ing__input"
              type="text"
              autofocus
              placeholder="Search by title, or paste an ISBN…"
              @input="onAddInput"
            />
          </form>
          <p class="addm__hint">
            {{
              looksLikeIsbn(addQuery)
                ? 'Looks like an ISBN — press Enter to add it.'
                : 'Type a title to search, or paste an ISBN.'
            }}
          </p>
          <ul v-if="search.length" class="addm__results">
            <li v-for="e in search" :key="e.id">
              <button class="ing__result" :disabled="busy" @click="onPickResult(e)">
                <span class="ing__result-title">{{ e.title }}</span>
                <span class="ing__result-meta"
                  >{{ e.author }}{{ e.year ? ` · ${e.year}` : '' }}</span
                >
              </button>
            </li>
          </ul>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.ing {
  max-width: 1520px;
  margin: 0 auto;
  padding: 20px 24px 60px;
  color: var(--text);
}

/* top bar */
.ing__bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 4px 18px;
  border-bottom: 1px solid var(--border);
}
.ing__brand {
  display: flex;
  align-items: center;
  gap: 10px;
  text-decoration: none;
  color: var(--text);
}
.ing__mark {
  width: 14px;
  height: 14px;
  border-radius: 3px;
  border: 2px solid var(--brass);
  transform: rotate(45deg);
}
.ing__name {
  font: 800 15px var(--font-display);
  letter-spacing: 0.16em;
}
.ing__crumb-sep {
  color: var(--dim);
}
.ing__crumb {
  font: 700 13px var(--font-ui);
  color: var(--muted);
}
.ing__crumb--active {
  color: var(--text);
}
.ing__bar-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 12px;
}
.ing__uploaded {
  font: 400 12.5px var(--font-ui);
  color: var(--dim);
}
.ing__avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: linear-gradient(135deg, var(--brass), #7a5d2c);
  color: var(--on-brass);
  font: 700 13px var(--font-display);
}

.ing__error {
  margin: 14px 0 0;
  padding: 10px 14px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--error) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--error) 40%, transparent);
  color: var(--error);
  font: 500 13px var(--font-ui);
}

.ing__eyebrow {
  font: 700 11px var(--font-display);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--brass);
}
.ing__h1 {
  font: 800 30px var(--font-display);
  letter-spacing: -0.01em;
  margin: 8px 0 0;
}
.ing__lead {
  font: 400 14.5px/1.6 var(--font-ui);
  color: var(--muted);
  max-width: 560px;
}
.ing__muted {
  color: var(--dim);
  font: 400 14px var(--font-ui);
}
.ing__muted--pad {
  padding: 24px 8px;
}

/* batch picker */
.ing__pick-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 24px;
  margin: 22px 0 20px;
}
.ing__batches {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ing__batch {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  border-radius: 12px;
  background: var(--surface);
  border: 1px solid var(--border);
  cursor: pointer;
  color: var(--text);
  text-align: left;
}
.ing__batch:hover {
  border-color: var(--border-strong);
}
.ing__batch-title {
  font: 700 14px var(--font-ui);
}
.ing__batch-meta {
  font: 400 12.5px var(--font-ui);
  color: var(--dim);
}
.ing__batch-status {
  margin-left: auto;
  font: 600 11px var(--font-ui);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--muted);
}
.ing__batch-status.is-confirmed {
  color: var(--success);
}

/* active-batch header */
.ing__head {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 20px;
  margin: 22px 0 16px;
}
.ing__title-row {
  display: flex;
  align-items: baseline;
  gap: 14px;
}
.ing__submeta {
  font: 400 14px var(--font-ui);
  color: var(--dim);
}
.ing__head-actions {
  display: flex;
  gap: 11px;
}

.ing__input {
  width: 100%;
  padding: 10px 13px;
  border-radius: 10px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  color: var(--text);
  font: 500 13.5px var(--font-ui);
}
.ing__input--num {
  width: 64px;
}
.ing__result {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 10px;
  border: none;
  background: transparent;
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
  color: var(--text);
}
.ing__result:hover {
  background: var(--hatch);
}
.ing__result-title {
  font: 600 13px var(--font-ui);
}
.ing__result-meta {
  font: 400 11.5px var(--font-ui);
  color: var(--dim);
}

/* split */
.ing__split {
  display: grid;
  grid-template-columns: 384px 1fr;
  gap: 0;
  border: 1px solid var(--border);
  border-radius: 14px;
  overflow: hidden;
  background: var(--surface);
  min-height: 560px;
}
.ing__queue {
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.ing__queue-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 16px 16px 12px;
}
.ing__chips {
  display: flex;
  gap: 8px;
  flex: 1;
  flex-wrap: wrap;
}
.ing__add-btn {
  flex: none;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  border: 1px solid var(--border-strong);
  background: var(--surface-raised);
  color: var(--text);
  font: 700 18px/1 var(--font-ui);
  cursor: pointer;
  display: grid;
  place-items: center;
}
.ing__add-btn:hover {
  border-color: var(--brass);
  color: var(--brass-bright);
}
.chip {
  padding: 6px 12px;
  border-radius: 999px;
  border: none;
  background: var(--hatch);
  color: var(--muted);
  font: 700 12px var(--font-ui);
  cursor: pointer;
}
.chip--on {
  background: var(--brass);
  color: var(--on-brass);
}
.ing__list {
  flex: 1;
  overflow: auto;
  padding: 0 12px 12px;
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--surface-raised);
  border: 1px solid var(--border);
  cursor: pointer;
  text-align: left;
  color: var(--text);
}
.row--sel {
  border-color: color-mix(in srgb, var(--brass) 55%, transparent);
  box-shadow: inset 3px 0 0 var(--brass);
}
.row--no_match {
  background: color-mix(in srgb, var(--error) 7%, var(--surface-raised));
}
.row__cover {
  width: 38px;
  height: 57px;
  flex: none;
  border-radius: 5px;
  overflow: hidden;
  background: var(--panel);
  display: grid;
  place-items: center;
}
.row__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.row__cover-fallback {
  font: 800 9px var(--font-display);
  color: var(--muted);
}
.row__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.row__title {
  font: 700 13.5px var(--font-ui);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row__sub {
  font: 400 11.5px var(--font-ui);
  color: var(--dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row__sub--mono {
  font-family: ui-monospace, monospace;
}
.row__status {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 3px;
  flex: none;
}
.row__status-label {
  font: 700 10.5px var(--font-ui);
}
.row__pct {
  font: 600 10px var(--font-ui);
  color: var(--dim);
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  display: inline-block;
}
.dot--ready {
  background: var(--success);
}
.dot--review {
  background: var(--brass);
}
.dot--no_match,
.dot--duplicate {
  background: var(--error);
}
.is-ready {
  color: var(--success);
}
.is-review {
  color: var(--brass-bright);
}
.is-no_match,
.is-duplicate {
  color: var(--error);
}

/* detail */
.ing__detail {
  padding: 24px 30px;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.ing__empty {
  margin: auto;
  color: var(--dim);
}
.detail__top {
  display: flex;
  gap: 30px;
}
.detail__cover {
  width: 200px;
  aspect-ratio: 2 / 3;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: var(--shadow-pop);
  border: 1px solid var(--border);
  background: var(--panel);
}
.detail__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.detail__cover-fallback {
  width: 100%;
  height: 100%;
  display: grid;
  place-items: center;
  padding: 16px;
  text-align: center;
  font: 800 20px/1.1 var(--font-display);
  color: var(--muted);
}
.detail__meta {
  flex: 1;
  min-width: 0;
}
.badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  border-radius: 999px;
  font: 700 11.5px var(--font-ui);
  border: 1px solid var(--border-strong);
}
.badge--ready {
  color: var(--success);
  background: color-mix(in srgb, var(--success) 14%, transparent);
  border-color: color-mix(in srgb, var(--success) 40%, transparent);
}
.badge--review {
  color: var(--brass-bright);
  background: color-mix(in srgb, var(--brass) 14%, transparent);
}
.badge--no_match,
.badge--duplicate {
  color: var(--error);
  background: color-mix(in srgb, var(--error) 12%, transparent);
}
.detail__title {
  font: 800 30px/1.05 var(--font-display);
  letter-spacing: -0.01em;
  margin: 14px 0 4px;
}
.detail__author {
  font: 400 16px var(--font-ui);
  color: var(--muted);
}
.detail__trace {
  margin-top: 12px;
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 9px 13px;
  border-radius: 9px;
  background: var(--hatch);
  border: 1px solid var(--border);
  font: 500 12.5px var(--font-ui);
  color: var(--muted);
}
.detail__trace-arrow {
  color: var(--brass);
}
.detail__trace-to {
  color: var(--brass-bright);
}
.mono {
  font-family: ui-monospace, monospace;
}
.detail__fields {
  margin-top: 20px;
  display: grid;
  /* minmax(0, 1fr): equal tracks even when a field value is long (LYCM-80). */
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 18px 26px;
}
.detail__fields dt,
.detail__series dt {
  font: 600 10px var(--font-ui);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--dim);
  margin-bottom: 5px;
}
.detail__fields dd {
  margin: 0;
  font: 500 14px var(--font-ui);
  color: var(--text);
}
.detail__series {
  margin-top: 22px;
  padding-top: 18px;
  border-top: 1px solid var(--border);
}
.detail__series-inputs {
  display: flex;
  gap: 8px;
  max-width: 420px;
}

/* edition picker */
.picker {
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid var(--border);
}
.picker__label {
  display: flex;
  align-items: center;
  gap: 8px;
  font: 700 11px var(--font-ui);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--brass-bright);
  margin-bottom: 14px;
}
.picker__grid {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
}
.edition {
  width: 140px;
  border: none;
  background: transparent;
  cursor: pointer;
  text-align: left;
  padding: 0;
  color: var(--text);
}
.edition__cover {
  aspect-ratio: 2 / 3;
  border-radius: 8px;
  overflow: hidden;
  background: var(--panel);
  display: grid;
  place-items: center;
  box-shadow: var(--shadow-card);
  font: 800 18px var(--font-display);
  color: var(--muted);
}
.edition__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.edition--on .edition__cover {
  box-shadow:
    0 0 0 2px var(--brass),
    var(--shadow-card);
}
.edition__title {
  margin-top: 9px;
  font: 700 12.5px var(--font-ui);
}
.edition__meta {
  font: 400 11px var(--font-ui);
  color: var(--dim);
}

.fallback {
  margin-top: 22px;
  padding: 14px 16px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--error) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--error) 28%, transparent);
  font: 400 13px/1.6 var(--font-ui);
  color: var(--muted);
}
.fallback__lead {
  margin: 0 0 12px;
}
.fallback__form {
  display: flex;
  gap: 8px;
  max-width: 420px;
}

/* add-book modal */
.addm__scrim {
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(8, 8, 7, 0.7);
  backdrop-filter: blur(3px);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 84px 24px 24px;
}
.addm {
  width: 520px;
  max-width: 100%;
  padding: 20px 22px 22px;
  border-radius: 16px;
  background: var(--surface-raised);
  border: 1px solid var(--border-strong);
  box-shadow: var(--shadow-pop);
}
.addm__head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}
.addm__back {
  width: 34px;
  height: 34px;
  flex: none;
  border-radius: 9px;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--text);
  font: 700 18px/1 var(--font-ui);
  cursor: pointer;
}
.addm__back:hover {
  border-color: var(--brass);
  color: var(--brass-bright);
}
.addm__title {
  font: 800 18px var(--font-display);
  margin: 0;
}
.addm__search {
  margin: 0;
}
.addm__hint {
  margin: 8px 2px 0;
  font: 400 12px var(--font-ui);
  color: var(--dim);
}
.addm__results {
  margin: 12px 0 0;
  padding: 6px;
  list-style: none;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
  max-height: 340px;
  overflow: auto;
}

.detail__actions {
  margin-top: auto;
  display: flex;
  align-items: center;
  gap: 12px;
  padding-top: 22px;
  border-top: 1px solid var(--border);
}
.detail__spacer {
  flex: 1;
}

/* buttons */
.btn {
  padding: 11px 20px;
  border-radius: 11px;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--text);
  font: 700 13.5px var(--font-ui);
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.btn--brass {
  background: var(--brass);
  color: var(--on-brass);
  border-color: transparent;
  font-weight: 800;
}
.btn--ghost {
  background: transparent;
}
.btn--danger {
  border-color: transparent;
  color: var(--error);
}

@media (max-width: 900px) {
  .ing__split {
    grid-template-columns: 1fr;
  }
  .detail__top {
    flex-direction: column;
  }
}
</style>
