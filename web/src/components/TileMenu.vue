<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'

// A small context menu anchored at a screen point, teleported to <body> so it
// escapes the card's clipping/stacking. Closes on select, outside click, scroll,
// or Escape.
defineProps<{ x: number; y: number; items: ReadonlyArray<{ key: string; label: string }> }>()
const emit = defineEmits<{ (e: 'select', key: string): void; (e: 'close'): void }>()

function onDocPointer(): void {
  emit('close')
}
function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => {
  // Defer so the opening contextmenu event doesn't immediately close it.
  setTimeout(() => {
    document.addEventListener('pointerdown', onDocPointer)
    document.addEventListener('scroll', onDocPointer, true)
    document.addEventListener('keydown', onKeydown)
  }, 0)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocPointer)
  document.removeEventListener('scroll', onDocPointer, true)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <Teleport to="body">
    <div class="tilemenu" :style="{ left: `${x}px`, top: `${y}px` }" @pointerdown.stop>
      <button
        v-for="item in items"
        :key="item.key"
        type="button"
        class="tilemenu__item"
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
</style>
