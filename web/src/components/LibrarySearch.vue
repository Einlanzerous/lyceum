<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import ModalDialog from './ModalDialog.vue'

// An argosy-style search: an overlay that opens above the shelf on demand
// (icon, "/", or Cmd/Ctrl-K) rather than a bar living permanently in the chrome.
// The shelf filters live underneath as you type (LYCM-63).
const props = defineProps<{ open: boolean; modelValue: string; resultCount: number }>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'close'): void
}>()

const input = ref<HTMLInputElement | null>(null)

watch(
  () => props.open,
  async (open) => {
    if (open) {
      await nextTick()
      input.value?.focus()
      input.value?.select()
    }
  },
)

function onInput(e: Event): void {
  emit('update:modelValue', (e.target as HTMLInputElement).value)
}

function clear(): void {
  emit('update:modelValue', '')
  input.value?.focus()
}
</script>

<template>
  <!-- A native modal <dialog> (LYCM-142): Esc, the backdrop click, the inert page -->
  <!-- and focus return to whatever opened the search all come from ModalDialog. -->
  <ModalDialog v-if="open" class="search" aria-label="Search your library" @close="emit('close')">
    <div class="search__panel">
      <div class="search__field">
        <span class="search__icon" aria-hidden="true">⌕</span>
        <input
          ref="input"
          :value="modelValue"
          type="text"
          class="search__input"
          placeholder="Search by title or author…"
          autocomplete="off"
          spellcheck="false"
          aria-label="Search your library"
          @input="onInput"
        />
        <button
          v-if="modelValue"
          type="button"
          class="search__clear"
          aria-label="Clear search"
          @click="clear"
        >
          ✕
        </button>
        <kbd class="search__hint">esc</kbd>
      </div>
      <div v-if="modelValue.trim()" class="search__meta">
        {{ resultCount }} {{ resultCount === 1 ? 'match' : 'matches' }}
      </div>
    </div>
  </ModalDialog>
</template>

<style scoped>
.search {
  --modal-bg: rgba(8, 8, 7, 0.44);
  --modal-align: flex-start;
  --modal-pad: clamp(60px, 14dvh, 140px) 0 0;
}
:root[data-theme='light'] .search {
  --modal-bg: rgba(28, 26, 23, 0.28);
}
.search__panel {
  position: relative;
  width: min(560px, 92vw);
}
.search__field {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-radius: 14px;
  border: 1px solid var(--border-strong);
  background: var(--surface-raised);
  box-shadow: var(--shadow-pop);
}
.search__icon {
  font-size: 20px;
  color: var(--brass);
}
.search__input {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  color: var(--text);
  font: 500 16px var(--font-ui);
}
.search__input::placeholder {
  color: var(--dim);
}
.search__clear {
  border: none;
  background: transparent;
  color: var(--dim);
  font-size: 13px;
  cursor: pointer;
  padding: 2px 6px;
}
.search__hint {
  font: 600 10px var(--font-ui);
  color: var(--dim);
  border: 1px solid var(--border-strong);
  border-radius: 5px;
  padding: 2px 6px;
}
.search__meta {
  margin-top: 10px;
  padding-left: 4px;
  font: 500 12px var(--font-ui);
  color: var(--muted);
}
</style>
