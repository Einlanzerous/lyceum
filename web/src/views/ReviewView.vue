<script setup lang="ts">
// Ingestion QC review queue (LYCM-58). Books that tripped an ingest detector
// (no ISBN, poor source cover, mangled title) are held off the shelf and land
// here. For each, the librarian can fix the title/author, replace the cover
// (re-fetch from the art source or upload a file), then approve it onto the
// shelf — or delete it outright.
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  listPendingReview,
  approveBook,
  updateBook,
  refetchCover,
  replaceCover,
  deleteBook,
} from '@/api/client'
import { coverSrc, invalidateCover } from '@/api/coverSrc'
import type { Book } from '@/api/types'

/** Human labels for the backend's stable flag codes. */
const FLAG_LABELS: Record<string, string> = {
  no_isbn: 'No ISBN',
  no_cover: 'No cover',
  low_quality_cover: 'Poor cover',
  suspicious_title: 'Odd title',
}
function flagLabel(code: string): string {
  return FLAG_LABELS[code] ?? code
}

const books = ref<Book[]>([])
const loading = ref(true)
const error = ref('')
// Per-book UI state: the edit fields and any inline busy/error status.
const drafts = ref<Record<number, { title: string; author: string }>>({})
const busy = ref<Record<number, string>>({}) // id -> action in flight ('' when idle)
const rowError = ref<Record<number, string>>({})
// Cache-buster so a replaced cover image reloads instead of showing the stale one.
const coverBust = ref<Record<number, number>>({})

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const list = await listPendingReview()
    books.value = list
    drafts.value = Object.fromEntries(
      list.map((b) => [b.id, { title: b.title, author: b.author }]),
    )
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load the review queue.'
  } finally {
    loading.value = false
  }
}

onMounted(load)

function coverSrcFor(b: Book): string {
  const bust = coverBust.value[b.id]
  // The bytes can change under a stable id (replace / re-fetch), so bust both
  // the browser cache and the native blob cache.
  return coverSrc(b.id) + (bust ? `?v=${bust}` : '')
}

/** Remove a row from the list once it leaves the queue (approve/delete). */
function drop(id: number): void {
  books.value = books.value.filter((b) => b.id !== id)
}

async function run(id: number, action: string, fn: () => Promise<void>): Promise<void> {
  busy.value = { ...busy.value, [id]: action }
  rowError.value = { ...rowError.value, [id]: '' }
  try {
    await fn()
  } catch (e) {
    rowError.value = {
      ...rowError.value,
      [id]: e instanceof Error ? e.message : `Could not ${action}.`,
    }
  } finally {
    busy.value = { ...busy.value, [id]: '' }
  }
}

function saveMeta(b: Book): Promise<void> {
  const d = drafts.value[b.id]
  return run(b.id, 'save', async () => {
    const updated = await updateBook(b.id, d.title.trim(), d.author.trim())
    b.title = updated.title
    b.author = updated.author
  })
}

function onRefetch(b: Book): Promise<void> {
  return run(b.id, 'refetch cover', async () => {
    await refetchCover(b.id)
    coverBust.value = { ...coverBust.value, [b.id]: Date.now() }
    invalidateCover(b.id)
    b.cover_url = coverSrc(b.id)
  })
}

function onUpload(b: Book, ev: Event): Promise<void> {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return Promise.resolve()
  return run(b.id, 'upload cover', async () => {
    await replaceCover(b.id, file)
    coverBust.value = { ...coverBust.value, [b.id]: Date.now() }
    invalidateCover(b.id)
    b.cover_url = coverSrc(b.id)
    input.value = '' // allow re-selecting the same file
  })
}

function onApprove(b: Book): Promise<void> {
  return run(b.id, 'approve', async () => {
    await approveBook(b.id)
    drop(b.id)
  })
}

function onDelete(b: Book): Promise<void> {
  if (!window.confirm(`Delete “${b.title}” permanently? This cannot be undone.`)) {
    return Promise.resolve()
  }
  return run(b.id, 'delete', async () => {
    await deleteBook(b.id)
    drop(b.id)
  })
}
</script>

<template>
  <section class="rev">
    <header class="rev__bar">
      <RouterLink to="/" class="rev__back" aria-label="Back to library">← Library</RouterLink>
      <h1 class="rev__title">Review queue</h1>
      <span class="rev__count" v-if="!loading && books.length">{{ books.length }}</span>
    </header>

    <p class="rev__intro">
      New ingests that tripped a quality check are held here. Fix the details, then approve them onto
      the shelf.
    </p>

    <div v-if="loading" class="rev__note">Loading…</div>
    <div v-else-if="error" class="rev__note rev__note--error">{{ error }}</div>
    <div v-else-if="!books.length" class="rev__note">
      Nothing to review — every ingested book is on the shelf. 🎉
    </div>

    <ul v-else class="rev__list">
      <li v-for="b in books" :key="b.id" class="card">
        <div class="card__cover">
          <img v-if="b.cover_url" :src="coverSrcFor(b)" :alt="`Cover of ${b.title}`" />
          <div v-else class="card__cover-empty">No cover</div>
        </div>

        <div class="card__body">
          <div class="card__flags">
            <span v-for="f in b.review_flags ?? []" :key="f" class="chip">{{ flagLabel(f) }}</span>
          </div>

          <label class="field">
            <span class="field__label">Title</span>
            <input v-model="drafts[b.id].title" type="text" class="field__input" />
          </label>
          <label class="field">
            <span class="field__label">Author</span>
            <input v-model="drafts[b.id].author" type="text" class="field__input" />
          </label>

          <div class="card__actions">
            <button
              type="button"
              class="btn"
              :disabled="!!busy[b.id]"
              @click="saveMeta(b)"
            >
              Save details
            </button>
            <button
              type="button"
              class="btn btn--ghost"
              :disabled="!!busy[b.id]"
              @click="onRefetch(b)"
            >
              Re-fetch cover
            </button>
            <label class="btn btn--ghost card__upload">
              Upload cover
              <input type="file" accept="image/*" @change="onUpload(b, $event)" hidden />
            </label>
            <span class="card__spacer" />
            <button
              type="button"
              class="btn btn--primary"
              :disabled="!!busy[b.id]"
              @click="onApprove(b)"
            >
              Approve
            </button>
            <button
              type="button"
              class="btn btn--danger"
              :disabled="!!busy[b.id]"
              @click="onDelete(b)"
            >
              Delete
            </button>
          </div>

          <p v-if="busy[b.id]" class="card__status">Working… ({{ busy[b.id] }})</p>
          <p v-else-if="rowError[b.id]" class="card__status card__status--error">
            {{ rowError[b.id] }}
          </p>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.rev {
  max-width: 880px;
  margin: 0 auto;
  padding: 24px 20px 64px;
  color: var(--text);
  font-family: var(--font-ui);
}
.rev__bar {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 8px;
}
.rev__back {
  color: var(--muted);
  text-decoration: none;
  font-size: 14px;
}
.rev__back:hover {
  color: var(--text);
}
.rev__title {
  font: 600 20px var(--font-display);
  margin: 0;
}
.rev__count {
  background: var(--brass);
  color: var(--on-brass);
  border-radius: 999px;
  padding: 1px 9px;
  font-size: 12px;
  font-weight: 700;
}
.rev__intro {
  color: var(--muted);
  font-size: 14px;
  margin: 0 0 20px;
}
.rev__note {
  padding: 40px 0;
  text-align: center;
  color: var(--muted);
}
.rev__note--error {
  color: var(--error);
}
.rev__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.card {
  display: flex;
  gap: 16px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 14px;
}
.card__cover {
  flex: 0 0 92px;
  width: 92px;
  aspect-ratio: 366 / 600;
  border-radius: 6px;
  overflow: hidden;
  background: var(--panel);
}
.card__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.card__cover-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  font-size: 11px;
  color: var(--dim);
  text-align: center;
  padding: 6px;
}
.card__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.card__flags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.chip {
  background: color-mix(in srgb, var(--error) 18%, transparent);
  color: var(--error);
  border: 1px solid color-mix(in srgb, var(--error) 30%, transparent);
  border-radius: 999px;
  padding: 2px 9px;
  font-size: 11px;
  font-weight: 600;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.field__label {
  font-size: 11px;
  color: var(--dim);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.field__input {
  background: var(--bg);
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  padding: 7px 9px;
  color: var(--text);
  font: inherit;
  font-size: 14px;
}
.field__input:focus {
  outline: none;
  border-color: var(--brass);
}
.card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 2px;
}
.card__spacer {
  flex: 1;
}
.btn {
  border: 1px solid var(--border-strong);
  background: var(--surface-raised);
  color: var(--text);
  border-radius: 7px;
  padding: 7px 12px;
  font: 600 13px var(--font-ui);
  cursor: pointer;
}
.btn:hover:not(:disabled) {
  border-color: var(--brass);
}
.btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.btn--ghost {
  background: transparent;
}
.btn--primary {
  background: var(--brass);
  border-color: var(--brass);
  color: var(--on-brass);
}
.btn--danger {
  color: var(--error);
  border-color: color-mix(in srgb, var(--error) 40%, transparent);
}
.card__upload {
  display: inline-flex;
  align-items: center;
}
.card__status {
  font-size: 12px;
  color: var(--muted);
  margin: 0;
}
.card__status--error {
  color: var(--error);
}
</style>
