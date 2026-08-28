<script setup lang="ts">
// Edit a shelf book's title, author and series (LYCM-129). Until now these were
// fixed at ingest: an EPUB with no series metadata stayed series-less for good,
// and the only fix was an UPDATE in Postgres. Opens from the tile menu.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { Book } from '@/api/types'
import type { BookPatch } from '@/api/client'
import { draftOf, patchOf } from '@/library/bookDraft'

const props = defineProps<{ book: Book }>()
const emit = defineEmits<{
  (e: 'save', id: number, patch: BookPatch): void
  (e: 'close'): void
}>()

const draft = ref(draftOf(props.book))
const canSave = computed(() => draft.value.title.trim() !== '')

function submit(): void {
  if (!canSave.value) return
  emit('save', props.book.id, patchOf(draft.value))
}

function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div
    class="scrim"
    role="dialog"
    aria-modal="true"
    aria-label="Edit book details"
    @click.self="emit('close')"
  >
    <form class="sheet" @submit.prevent="submit">
      <h2 class="sheet__title">Edit details</h2>

      <label class="field">
        <span class="field__label">Title</span>
        <input v-model="draft.title" type="text" class="field__input" autofocus />
      </label>
      <label class="field">
        <span class="field__label">Author</span>
        <input v-model="draft.author" type="text" class="field__input" />
      </label>
      <div class="field-row">
        <label class="field field--grow">
          <span class="field__label">Series</span>
          <input v-model="draft.series" type="text" class="field__input" placeholder="None" />
        </label>
        <label class="field field--index">
          <span class="field__label">No.</span>
          <input
            v-model="draft.seriesIndex"
            type="text"
            inputmode="decimal"
            class="field__input"
            placeholder="—"
          />
        </label>
      </div>

      <div class="sheet__actions">
        <button type="button" class="btn btn--ghost" @click="emit('close')">Cancel</button>
        <button type="submit" class="btn" :disabled="!canSave">Save</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  z-index: 70;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgba(8, 8, 7, 0.62);
  backdrop-filter: blur(2px);
}
.sheet {
  width: min(440px, 100%);
  padding: 22px 24px 20px;
  border-radius: 12px;
  background: var(--panel);
  border: 1px solid rgba(201, 154, 78, 0.35);
  box-shadow: var(--shadow-card);
  display: grid;
  gap: 12px;
}
.sheet__title {
  margin: 0 0 4px;
  font: 800 17px var(--font-display);
  color: var(--text);
}
.field {
  display: grid;
  gap: 4px;
}
.field__label {
  font: 600 11px var(--font-ui);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--dim);
}
.field__input {
  width: 100%;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid var(--border-strong);
  background: var(--surface-raised);
  color: var(--text);
  font: 400 14px var(--font-ui);
}
.field__input:focus {
  outline: none;
  border-color: var(--brass);
}
.field-row {
  display: flex;
  gap: 10px;
}
.field--grow {
  flex: 1;
  min-width: 0;
}
.field--index {
  flex: 0 0 72px;
}
.sheet__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 6px;
}
.btn {
  padding: 8px 16px;
  border-radius: 999px;
  border: 1px solid rgba(201, 154, 78, 0.4);
  background: rgba(201, 154, 78, 0.12);
  color: var(--brass-bright);
  font: 700 12px var(--font-ui);
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.btn--ghost {
  border-color: var(--border-strong);
  background: transparent;
  color: var(--muted);
}
</style>
