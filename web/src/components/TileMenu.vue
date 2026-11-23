<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { menuItems, navigateMenu } from '@/library/menuNav'

// A small context menu anchored at a screen point, teleported to <body> so it
// escapes the card's clipping/stacking. Closes on select, outside click, scroll,
// Escape or Tab. Focus moves into it on open (arrow keys, Home and End move
// between entries) and goes back to where it was on close (LYCM-142).
defineProps<{
  x: number
  y: number
  items: ReadonlyArray<{ key: string; label: string; danger?: boolean }>
}>()
const emit = defineEmits<{ (e: 'select', key: string): void; (e: 'close'): void }>()

const menu = ref<HTMLElement | null>(null)
let opener: HTMLElement | null = null

function onMenuKey(e: KeyboardEvent): void {
  if (navigateMenu(e, menuItems(menu.value, '[role="menuitem"]'))) return
  if (e.key === 'Tab') {
    e.preventDefault()
    emit('close')
  }
}

function onDocPointer(): void {
  emit('close')
}
function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => {
  opener = document.activeElement as HTMLElement | null
  menuItems(menu.value, '[role="menuitem"]')[0]?.focus()
  // Defer so the opening contextmenu event doesn't immediately close it.
  setTimeout(() => {
    document.addEventListener('pointerdown', onDocPointer)
    document.addEventListener('scroll', onDocPointer, true)
    document.addEventListener('keydown', onKeydown)
  }, 0)
})
onBeforeUnmount(() => {
  // Only reclaim focus if it was ours: an outside click has already moved it.
  const mine = menu.value?.contains(document.activeElement)
  if (mine && opener?.isConnected) opener.focus()
  document.removeEventListener('pointerdown', onDocPointer)
  document.removeEventListener('scroll', onDocPointer, true)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="menu"
      class="tilemenu"
      role="menu"
      :style="{ left: `${x}px`, top: `${y}px` }"
      @pointerdown.stop
      @keydown="onMenuKey"
    >
      <button
        v-for="item in items"
        :key="item.key"
        type="button"
        role="menuitem"
        tabindex="-1"
        class="tilemenu__item"
        :class="{ 'tilemenu__item--danger': item.danger }"
        @click.prevent.stop="emit('select', item.key)"
      >
        {{ item.label }}
      </button>
    </div>
  </Teleport>
</template>

<style scoped>
.tilemenu {
  position: fixed;
  z-index: 60;
  min-width: 160px;
  padding: 6px;
  border-radius: 10px;
  border: 1px solid var(--border-strong);
  background: var(--surface-raised);
  box-shadow: var(--shadow-pop);
}
.tilemenu__item {
  display: block;
  width: 100%;
  padding: 9px 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text);
  font: 600 13px var(--font-ui);
  text-align: left;
  cursor: pointer;
}
.tilemenu__item:hover {
  background: var(--surface);
}
/* Destructive entries read as destructive before they are clicked. */
.tilemenu__item--danger {
  color: var(--error);
}
</style>
