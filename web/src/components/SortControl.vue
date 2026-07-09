<script setup lang="ts">
import { computed } from 'vue'
import { SORT_OPTIONS, type SortDir, type SortKey } from '@/library/sort'

const props = defineProps<{ sortKey: SortKey; dir: SortDir }>()
const emit = defineEmits<{
  (e: 'update:sortKey', key: SortKey): void
  (e: 'update:dir', dir: SortDir): void
}>()

const dirLabel = computed(() => (props.dir === 'asc' ? 'Ascending' : 'Descending'))

function onSelect(e: Event): void {
  emit('update:sortKey', (e.target as HTMLSelectElement).value as SortKey)
}
function toggleDir(): void {
  emit('update:dir', props.dir === 'asc' ? 'desc' : 'asc')
}
</script>

<template>
  <div class="sort" role="group" aria-label="Sort library">
    <label class="sort__select">
      <span class="sort__label">Sort</span>
      <select :value="sortKey" class="sort__native" aria-label="Sort by" @change="onSelect">
        <option v-for="opt in SORT_OPTIONS" :key="opt.key" :value="opt.key">{{ opt.label }}</option>
      </select>
      <span class="sort__caret" aria-hidden="true">▾</span>
    </label>
    <button
      type="button"
      class="sort__dir"
      :aria-label="`Direction: ${dirLabel}`"
      :title="dirLabel"
      @click="toggleDir"
    >
      {{ dir === 'asc' ? '↑' : '↓' }}
    </button>
  </div>
</template>

<style scoped>
.sort {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
}
.sort__select {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 30px 0 13px;
  height: 36px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
}
.sort__label {
  font: 700 11px var(--font-ui);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--dim);
}
.sort__native {
  appearance: none;
  border: none;
  background: transparent;
  color: var(--text);
  font: 600 13px var(--font-ui);
  cursor: pointer;
  outline: none;
  padding: 0;
}
.sort__native option {
  color: initial;
}
.sort__caret {
  position: absolute;
  right: 12px;
  color: var(--dim);
  font-size: 10px;
  pointer-events: none;
}
.sort__dir {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  color: var(--text);
  font-size: 14px;
  cursor: pointer;
}
.sort__dir:hover {
  border-color: var(--brass);
  color: var(--brass);
}
</style>
