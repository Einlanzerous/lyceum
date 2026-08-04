<script setup lang="ts">
// Ingestion QC review queue (LYCM-58). Books that tripped an ingest detector
// (no ISBN, poor source cover, mangled title) are held off the shelf and land
// here. For each, the librarian can fix the title/author, replace the cover
// (re-fetch from the art source or upload a file), then approve it onto the
// shelf — or delete it outright.
//
// A book held as a possible duplicate (LYCM-113) also shows the book it matched,
// side by side, because that decision cannot be made from one cover alone: two
// files of one work are often deliberate — a better scan, a different
// translation — so the question is "keep both?", not "is this junk?".
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  listPendingReview,
  approveBook,
  updateBook,
  refetchCover,
  replaceCover,
  deleteBook,
  getBook,
} from '@/api/client'
import { coverSrc, invalidateCover } from '@/api/coverSrc'
import type { Book } from '@/api/types'

/** Human labels for the backend's stable flag codes. */
const FLAG_LABELS: Record<string, string> = {
  no_isbn: 'No ISBN',
  no_cover: 'No cover',
  low_quality_cover: 'Poor cover',
  suspicious_title: 'Odd title',
  possible_duplicate: 'Possible duplicate',
}
function flagLabel(code: string): string {
  return FLAG_LABELS[code] ?? code
}

/**
 * Whether a book is held as a suspected duplicate.
 *
 * Keyed on the flag, not on `duplicate_of`. The pointer is nulled when the book
 * it named is deleted and the field is `omitempty`, so gating on it would hide
 * the panel in exactly the case the panel exists to explain — the flag outlives
 * the pointer by design.
 */
function isDuplicate(b: Book): boolean {
  return (b.review_flags ?? []).includes('possible_duplicate')
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
// For a book flagged as a possible duplicate: the book it matched, so the two
// can be compared. `null` means that book has since been deleted — the flag
// outlives the pointer by design, and saying so is better than showing nothing.
const matches = ref<Record<number, Book | null>>({})

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const list = await listPendingReview()
    books.value = list
    drafts.value = Object.fromEntries(list.map((b) => [b.id, { title: b.title, author: b.author }]))
    void loadMatches(list)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load the review queue.'
  } finally {
    loading.value = false
  }
}

/**
 * Fetches the counterpart of every suspected duplicate, concurrently and after
 * the queue has already rendered: the comparison is worth waiting a moment for,
 * but not worth holding the whole queue behind.
 */
async function loadMatches(list: Book[]): Promise<void> {
  const flagged = list.filter((b) => b.duplicate_of)
  // Three copies of one book point at the same original, so fetch each target
  // once rather than once per row holding it.
  const targets = [...new Set(flagged.map((b) => b.duplicate_of as number))]
  const fetched = new Map(
    await Promise.all(
      targets.map(async (id) => {
        try {
          return [id, await getBook(id)] as const
        } catch {
          // Deleted between the ingest and now — resolving one duplicate by
          // deleting the older book is exactly what this queue is for.
          return [id, null] as const
        }
      }),
    ),
  )
  matches.value = {
    ...matches.value,
    ...Object.fromEntries(
      flagged.map((b) => [b.id, fetched.get(b.duplicate_of as number) ?? null]),
    ),
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
  const remaining = { ...matches.value }
  delete remaining[id]
  matches.value = remaining
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
      <span v-if="!loading && books.length" class="rev__count">{{ books.length }}</span>
    </header>

    <p class="rev__intro">
      New ingests that tripped a quality check are held here. Fix the details, then approve them
      onto the shelf.
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

          <div v-if="isDuplicate(b)" class="dup">
            <p class="dup__lead">This looks like another copy of a book you already have.</p>
            <div v-if="matches[b.id]" class="dup__pair">
              <div class="dup__side">
                <span class="dup__tag">Already on the shelf</span>
                <img
                  v-if="matches[b.id]?.cover_url"
                  :src="coverSrc(matches[b.id]!.id)"
                  :alt="`Cover of ${matches[b.id]!.title}`"
                  class="dup__cover"
                />
                <div v-else class="dup__cover dup__cover--empty">No cover</div>
                <p class="dup__title">{{ matches[b.id]!.title }}</p>
                <p class="dup__author">{{ matches[b.id]!.author }}</p>
              </div>
              <div class="dup__side">
                <span class="dup__tag dup__tag--new">This one, held</span>
                <img
                  v-if="b.cover_url"
                  :src="coverSrcFor(b)"
                  :alt="`Cover of ${b.title}`"
                  class="dup__cover"
                />
                <div v-else class="dup__cover dup__cover--empty">No cover</div>
                <p class="dup__title">{{ b.title }}</p>
                <p class="dup__author">{{ b.author }}</p>
              </div>
            </div>
            <p
              v-else-if="!b.duplicate_of || matches[b.id] === null"
              class="dup__lead dup__lead--gone"
            >
              The book this matched has since been deleted, so there is probably nothing left to
              decide — approve it onto the shelf.
            </p>
            <p v-else class="dup__lead">Loading the other copy…</p>
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
            <button type="button" class="btn" :disabled="!!busy[b.id]" @click="saveMeta(b)">
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
              <input type="file" accept="image/*" hidden @change="onUpload(b, $event)" />
            </label>
            <span class="card__spacer" />
            <button
              type="button"
              class="btn btn--primary"
              :disabled="!!busy[b.id]"
              @click="onApprove(b)"
            >
              {{ isDuplicate(b) ? 'Keep both' : 'Approve' }}
            </button>
            <button
              type="button"
              class="btn btn--danger"
              :disabled="!!busy[b.id]"
              @click="onDelete(b)"
            >
              {{ isDuplicate(b) ? 'Delete this copy' : 'Delete' }}
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
.dup {
  border: 1px solid var(--rule, rgba(128, 128, 128, 0.35));
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 12px;
}
.dup__lead {
  margin: 0 0 10px;
  font-size: 14px;
  color: var(--muted);
}
.dup__lead--gone {
  margin-bottom: 0;
}
.dup__pair {
  display: grid;
  /* minmax(0, 1fr) rather than 1fr: a long unbroken title otherwise sets the
     column's min-content width and blows the grid out of its container. */
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.dup__side {
  min-width: 0;
}
.dup__tag {
  display: inline-block;
  font-size: 11px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--muted);
  margin-bottom: 6px;
}
.dup__tag--new {
  color: var(--brass);
}
.dup__cover {
  display: block;
  width: 100%;
  max-width: 110px;
  aspect-ratio: 366 / 600;
  object-fit: cover;
  border-radius: 4px;
  background: rgba(128, 128, 128, 0.15);
}
.dup__cover--empty {
  display: grid;
  place-items: center;
  font-size: 11px;
  color: var(--muted);
}
.dup__title {
  margin: 8px 0 0;
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.dup__author {
  margin: 2px 0 0;
  font-size: 13px;
  color: var(--muted);
  overflow-wrap: anywhere;
}

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
