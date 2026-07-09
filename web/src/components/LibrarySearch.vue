<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

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
  <Teleport to="body">
    <Transition name="search">
      <div v-if="open" class="search" @keydown.esc.prevent="emit('close')">
        <div class="search__backdrop" @click="emit('close')" />
        <div class="search__panel" role="dialog" aria-label="Search your library">
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
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.search {
  position: fixed;
  inset: 0;
  z-index: 50;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding-top: clamp(60px, 14vh, 140px);
}
.search__backdrop {
  position: absolute;
  inset: 0;
  background: rgba(8, 8, 7, 0.44);
  backdrop-filter: blur(2px);
}
:root[data-theme='light'] .search__backdrop {
  background: rgba(28, 26, 23, 0.28);
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
  outline: none;
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

.search-enter-active,
.search-leave-active {
  transition: opacity 0.16s ease;
}
.search-enter-active .search__panel,
.search-leave-active .search__panel {
  transition:
    transform 0.16s ease,
    opacity 0.16s ease;
}
.search-enter-from,
.search-leave-to {
  opacity: 0;
}
.search-enter-from .search__panel,
.search-leave-to .search__panel {
  transform: translateY(-8px);
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .search-enter-active,
  .search-leave-active,
  .search-enter-active .search__panel,
  .search-leave-active .search__panel {
    transition: none;
  }
}
</style>
