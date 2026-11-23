<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { SORT_OPTIONS, type SortDir, type SortKey } from '@/library/sort'
import { menuItems, navigateMenu } from '@/library/menuNav'

const props = defineProps<{ sortKey: SortKey; dir: SortDir }>()
const emit = defineEmits<{
  (e: 'update:sortKey', key: SortKey): void
  (e: 'update:dir', dir: SortDir): void
}>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)

const OPTIONS = '[role="option"]'

const currentLabel = computed(
  () => SORT_OPTIONS.find((o) => o.key === props.sortKey)?.label ?? 'Sort',
)
const dirLabel = computed(() => (props.dir === 'asc' ? 'Ascending' : 'Descending'))

// Opening lands focus on the current option; closing from the keyboard (a pick or
// Escape) hands it back to the trigger.
async function openMenu(): Promise<void> {
  open.value = true
  await nextTick()
  const opts = menuItems(menu.value, OPTIONS)
  ;(opts.find((o) => o.getAttribute('aria-selected') === 'true') ?? opts[0])?.focus()
}
function closeToTrigger(): void {
  open.value = false
  trigger.value?.focus()
}
function toggle(): void {
  if (open.value) open.value = false
  else void openMenu()
}
function select(key: SortKey): void {
  emit('update:sortKey', key)
  closeToTrigger()
}
function onTriggerKey(e: KeyboardEvent): void {
  if (e.key === 'ArrowDown' && !open.value) {
    e.preventDefault()
    void openMenu()
  }
}
function onMenuKey(e: KeyboardEvent): void {
  if (navigateMenu(e, menuItems(menu.value, OPTIONS))) return
  if (e.key === 'Tab') open.value = false
}
function toggleDir(): void {
  emit('update:dir', props.dir === 'asc' ? 'desc' : 'asc')
}

// Close on outside click / Escape while open.
function onDocClick(e: MouseEvent): void {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
function onKeydown(e: KeyboardEvent): void {
  if (open.value && e.key === 'Escape') closeToTrigger()
}
onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div ref="root" class="sort" role="group" aria-label="Sort library">
    <button
      ref="trigger"
      type="button"
      class="sort__trigger"
      :class="{ 'is-open': open }"
      :aria-expanded="open"
      aria-haspopup="listbox"
      @click="toggle"
      @keydown="onTriggerKey"
    >
      <span class="sort__eyebrow">Sort</span>
      <span class="sort__value">{{ currentLabel }}</span>
      <span class="sort__caret" aria-hidden="true">▾</span>
    </button>

    <button
      type="button"
      class="sort__dir"
      :aria-label="`Direction: ${dirLabel}`"
      :title="dirLabel"
      @click="toggleDir"
    >
      {{ dir === 'asc' ? '↑' : '↓' }}
    </button>

    <Transition name="sort-pop">
      <ul
        v-if="open"
        ref="menu"
        class="sort__menu"
        role="listbox"
        aria-label="Sort by"
        @keydown="onMenuKey"
      >
        <li v-for="opt in SORT_OPTIONS" :key="opt.key" role="presentation">
          <button
            type="button"
            class="sort__opt"
            :class="{ 'is-active': opt.key === sortKey }"
            role="option"
            tabindex="-1"
            :aria-selected="opt.key === sortKey"
            @click="select(opt.key)"
          >
            <span class="sort__tick" aria-hidden="true">{{ opt.key === sortKey ? '✓' : '' }}</span>
            {{ opt.label }}
          </button>
        </li>
      </ul>
    </Transition>
  </div>
</template>

<style scoped>
.sort {
  position: relative;
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
}

.sort__trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  cursor: pointer;
  color: var(--text);
}
.sort__trigger:hover,
.sort__trigger.is-open {
  border-color: var(--brass);
}
.sort__eyebrow {
  font: 700 11px var(--font-ui);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--dim);
}
.sort__value {
  font: 600 13px var(--font-ui);
  color: var(--text);
}
.sort__caret {
  font-size: 10px;
  color: var(--dim);
  transition: transform 0.15s ease;
}
.sort__trigger.is-open .sort__caret {
  transform: rotate(180deg);
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

/* Popover menu */
.sort__menu {
  position: absolute;
  top: calc(100% + 8px);
  left: 0;
  z-index: 20;
  min-width: 190px;
  margin: 0;
  padding: 6px;
  list-style: none;
  border-radius: 12px;
  border: 1px solid var(--border-strong);
  background: var(--surface-raised);
  box-shadow: var(--shadow-pop);
}
.sort__opt {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 9px 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text);
  font: 600 13px var(--font-ui);
  text-align: left;
  cursor: pointer;
}
.sort__opt:hover {
  background: var(--surface);
}
.sort__opt.is-active {
  color: var(--brass);
}
.sort__tick {
  width: 12px;
  font-size: 11px;
  color: var(--brass);
}

.sort-pop-enter-active,
.sort-pop-leave-active {
  transition:
    opacity 0.14s ease,
    transform 0.14s ease;
  transform-origin: top left;
}
.sort-pop-enter-from,
.sort-pop-leave-to {
  opacity: 0;
  transform: translateY(-4px) scale(0.98);
}
@media (prefers-reduced-motion: reduce) {
  .sort-pop-enter-active,
  .sort-pop-leave-active {
    transition: none;
  }
}
</style>
