<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import BookCard from '@/components/BookCard.vue'
import { useLibraryStore } from '@/stores/library'

const store = useLibraryStore()
const { books, loading, error } = storeToRefs(store)

const fileInput = ref<HTMLInputElement | null>(null)
const dragging = ref(false)
const toast = ref<string | null>(null)
let toastTimer: ReturnType<typeof setTimeout> | undefined

onMounted(() => store.load())

function showToast(message: string): void {
  toast.value = message
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toast.value = null), 3000)
}

// Only EPUBs are accepted; the backend rejects the rest, but filtering here
// keeps the toast accurate and avoids pointless round-trips.
function epubsFrom(list: FileList | null | undefined): File[] {
  if (!list) return []
  return Array.from(list).filter(
    (f) => f.type === 'application/epub+zip' || f.name.toLowerCase().endsWith('.epub'),
  )
}

async function ingest(files: File[]): Promise<void> {
  if (files.length === 0) return
  const results = await store.uploadMany(files)
  const added = results.filter((r) => r.kind === 'added').length
  const duplicates = results.filter((r) => r.kind === 'duplicate').length
  const failed = results.filter((r) => r.kind === 'error').length

  const parts: string[] = []
  if (added) parts.push(`${added} added`)
  if (duplicates) parts.push(`${duplicates} already in your library`)
  if (failed) parts.push(`${failed} failed`)
  showToast(parts.join(' · ') || 'nothing to add')
}

function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  void ingest(epubsFrom(input.files))
  input.value = '' // allow re-picking the same file
}

function onDrop(event: DragEvent): void {
  dragging.value = false
  void ingest(epubsFrom(event.dataTransfer?.files))
}
</script>

<template>
  <section
    class="library"
    :class="{ 'library--dragging': dragging }"
    @dragover.prevent="dragging = true"
    @dragleave.prevent="dragging = false"
    @drop.prevent="onDrop"
  >
    <header class="library__bar">
      <h1>Library</h1>
      <button type="button" class="library__add" @click="fileInput?.click()">Add EPUB</button>
      <input
        ref="fileInput"
        type="file"
        accept="application/epub+zip,.epub"
        multiple
        hidden
        @change="onPick"
      />
    </header>

    <p v-if="loading" class="placeholder">Loading…</p>
    <p v-else-if="error" class="library__error" role="alert">{{ error }}</p>
    <p v-else-if="books.length === 0" class="placeholder">
      No books yet. Drop an EPUB here or use “Add EPUB”.
    </p>

    <div v-else class="library__grid">
      <BookCard v-for="book in books" :key="book.id" :book="book" />
    </div>

    <div class="library__dropzone" aria-hidden="true">Drop EPUBs to add</div>
    <output v-if="toast" class="library__toast">{{ toast }}</output>
  </section>
</template>

<style scoped>
.library {
  position: relative;
  min-height: 100%;
}

.library__bar {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1rem;
}

.library__bar h1 {
  margin: 0;
  font-size: 1.4rem;
}

.library__add {
  margin-left: auto;
  padding: 0.45rem 0.9rem;
  border: 0;
  border-radius: 6px;
  background: var(--accent);
  color: #fff;
  font-weight: 600;
  cursor: pointer;
}

.library__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 1.25rem;
}

.library__error {
  color: #b00020;
}

.library__dropzone {
  display: none;
}

.library--dragging .library__dropzone {
  display: flex;
  align-items: center;
  justify-content: center;
  position: fixed;
  inset: 0;
  background: rgba(124, 58, 237, 0.12);
  border: 3px dashed var(--accent);
  font-size: 1.2rem;
  font-weight: 600;
  color: var(--accent);
  pointer-events: none;
  z-index: 10;
}

.library__toast {
  position: fixed;
  left: 50%;
  bottom: 1.5rem;
  transform: translateX(-50%);
  padding: 0.6rem 1rem;
  border-radius: 8px;
  background: #1c1a17;
  color: #fff;
  font-size: 0.9rem;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
  z-index: 20;
}
</style>
