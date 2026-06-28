<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useReader } from '@/reader/useReader'
import { createPositionSync } from '@/reader/sync'
import { useUnloadFlush } from '@/reader/useUnloadFlush'
import { putPositionKeepalive } from '@/api/client'
import { formatProgress } from '@/api/progress'

const props = defineProps<{ id: string }>()
const router = useRouter()
const bookId = computed(() => Number(props.id))

const container = ref<HTMLElement | null>(null)

// Position sync: restore the last page on open, debounce-save every page turn,
// and flush immediately when leaving the reader.
const sync = createPositionSync(bookId.value, { putKeepalive: putPositionKeepalive })
const reader = useReader(container, bookId.value, {
  initialCfi: () => sync.restore(),
  onRelocate: (info) => sync.schedule(info),
})

// On tab close / backgrounding, keepalive-PUT the latest position immediately
// rather than waiting out the debounce.
useUnloadFlush(() => {
  const pos = reader.currentPosition()
  if (pos) sync.flushOnUnload(pos)
})

// Window-level keyboard nav (the composable also handles keys from inside the
// iframe). Left/right turn pages; Escape returns to the library.
function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'ArrowRight') reader.next()
  else if (event.key === 'ArrowLeft') reader.prev()
  else if (event.key === 'Escape') void router.push('/')
}

// Minimal horizontal swipe → page turn.
let touchStartX = 0
function onTouchStart(event: TouchEvent): void {
  touchStartX = event.changedTouches[0]?.clientX ?? 0
}
function onTouchEnd(event: TouchEvent): void {
  const dx = (event.changedTouches[0]?.clientX ?? 0) - touchStartX
  if (Math.abs(dx) < 40) return
  if (dx < 0) reader.next()
  else reader.prev()
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  void sync.flush()
  sync.dispose()
  reader.destroy()
})
</script>

<template>
  <div
    class="reader"
    :data-cfi="reader.cfi.value"
    :data-progress="reader.progress.value"
    @touchstart.passive="onTouchStart"
    @touchend.passive="onTouchEnd"
  >
    <header class="reader__bar">
      <button type="button" class="reader__btn" title="Back to library" @click="router.push('/')">
        ← Library
      </button>
      <div class="reader__spacer" />
      <button type="button" class="reader__btn" title="Smaller text" @click="reader.decreaseFont()">
        A−
      </button>
      <button type="button" class="reader__btn" title="Larger text" @click="reader.increaseFont()">
        A+
      </button>
      <button type="button" class="reader__btn" title="Toggle theme" @click="reader.toggleTheme()">
        {{ reader.theme.value === 'dark' ? '☀' : '☾' }}
      </button>
      <span class="reader__progress">{{ formatProgress(reader.progress.value) }}</span>
    </header>

    <div class="reader__stage">
      <button
        type="button"
        class="reader__nav reader__nav--prev"
        :disabled="reader.atStart.value"
        aria-label="Previous page"
        @click="reader.prev()"
      >
        ‹
      </button>

      <div ref="container" class="reader__surface"></div>

      <button
        type="button"
        class="reader__nav reader__nav--next"
        :disabled="reader.atEnd.value"
        aria-label="Next page"
        @click="reader.next()"
      >
        ›
      </button>

      <p v-if="reader.loading.value" class="reader__overlay">Opening book…</p>
      <p v-else-if="reader.error.value" class="reader__overlay reader__overlay--error" role="alert">
        {{ reader.error.value }}
      </p>
    </div>
  </div>
</template>

<style scoped>
.reader {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.reader__bar {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.4rem 0.6rem;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  background: var(--bg);
}

.reader__spacer {
  flex: 1;
}

.reader__btn {
  padding: 0.3rem 0.6rem;
  border: 1px solid rgba(0, 0, 0, 0.15);
  border-radius: 6px;
  background: transparent;
  color: inherit;
  font-size: 0.85rem;
  cursor: pointer;
}

.reader__progress {
  min-width: 3rem;
  text-align: right;
  font-size: 0.8rem;
  color: var(--muted);
}

.reader__stage {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: stretch;
}

.reader__surface {
  flex: 1;
  min-width: 0;
  height: 100%;
}

.reader__nav {
  flex: 0 0 auto;
  width: 3rem;
  border: 0;
  background: transparent;
  font-size: 2rem;
  color: var(--muted);
  cursor: pointer;
}

.reader__nav:disabled {
  opacity: 0.25;
  cursor: default;
}

.reader__overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0;
  color: var(--muted);
  background: var(--bg);
}

.reader__overlay--error {
  color: #b00020;
}
</style>
