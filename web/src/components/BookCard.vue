<script setup lang="ts">
import { computed, ref } from 'vue'
import { coverUrl } from '@/api/client'
import { formatProgress } from '@/api/progress'
import type { Book } from '@/api/types'
import TileMenu from '@/components/TileMenu.vue'

const props = defineProps<{ book: Book; pinned?: boolean }>()
const emit = defineEmits<{ (e: 'set-finished', id: number, finished: boolean): void }>()

// Covers can 404 (e.g. a missing blob); fall back to the title treatment.
const coverFailed = ref(false)

const finished = computed(() => props.book.finished === true)
const hasProgress = computed(() => typeof props.book.progress === 'number')
const progressPct = computed(() =>
  hasProgress.value ? Math.round((props.book.progress as number) * 100) : 0,
)
const progressLabel = computed(() =>
  hasProgress.value ? formatProgress(props.book.progress as number) : '',
)
const cover = computed(() =>
  props.book.cover_url && !coverFailed.value ? coverUrl(props.book.id) : '',
)

// contextmenu fires on desktop right-click AND Android long-press, so one
// handler gives the tile its "mark as read" affordance on both.
const menu = ref<{ x: number; y: number } | null>(null)
function openMenu(e: MouseEvent): void {
  menu.value = { x: e.clientX, y: e.clientY }
}
function toggleFinished(): void {
  emit('set-finished', props.book.id, !finished.value)
  menu.value = null
}
</script>

<template>
  <RouterLink
    :to="`/reader/${book.id}`"
    class="card"
    :title="book.title"
    @contextmenu.prevent="openMenu"
  >
    <!-- Cover is the hero. A 1px hatch unifies mixed-quality art. -->
    <div class="card__cover" :class="{ 'card__cover--fallback': !cover }">
      <img
        v-if="cover"
        :src="cover"
        :alt="`Cover of ${book.title}`"
        loading="lazy"
        class="card__img"
        @error="coverFailed = true"
      />
      <template v-else>
        <span class="card__tick" :class="{ 'card__tick--active': hasProgress }" />
        <span class="card__fallback-title">{{ book.title }}</span>
        <span class="card__fallback-author">{{ book.author }}</span>
        <!-- Hatch only textures the generated fallback tile; never real art. -->
        <div class="card__hatch" aria-hidden="true" />
      </template>

      <span v-if="pinned && !finished" class="card__continue">▸ Continue</span>
      <span v-if="finished" class="card__read">✓ Read</span>
      <span v-else-if="hasProgress" class="card__pill">{{ progressLabel }}</span>
      <div v-if="hasProgress && !finished" class="card__seam" aria-hidden="true">
        <div class="card__seam-fill" :style="{ width: progressPct + '%' }" />
      </div>
    </div>

    <div class="card__title">{{ book.title }}</div>
    <div class="card__author">{{ book.author }}</div>

    <TileMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :items="[{ key: 'finish', label: finished ? 'Mark as unread' : 'Mark as read' }]"
      @select="toggleFinished"
      @close="menu = null"
    />
  </RouterLink>
</template>

<style scoped>
.card {
  display: block;
  text-decoration: none;
  color: inherit;
}

.card__cover {
  position: relative;
  /* Matches the aspect of our cover source (Apple Books, ~366x600) so covers
     fill the card edge-to-edge with no letterbox bars. */
  aspect-ratio: 366 / 600;
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid var(--border);
  background: var(--surface-raised);
  box-shadow: var(--shadow-card);
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease;
}

.card:hover .card__cover {
  transform: translateY(-4px);
  box-shadow: 0 12px 26px rgba(0, 0, 0, 0.45);
}

.card__img {
  width: 100%;
  height: 100%;
  /* cover, with the card aspect matched to the source above: covers fill
     edge-to-edge. Any residual aspect difference crops the sides (safe — the
     top banner and bottom author bar are never clipped). */
  object-fit: cover;
  display: block;
}

.card__hatch {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: repeating-linear-gradient(135deg, var(--hatch) 0 2px, transparent 2px 9px);
}

.card__cover--fallback .card__hatch {
  background: repeating-linear-gradient(135deg, var(--hatch-fallback) 0 2px, transparent 2px 11px);
}

/* Fallback (no cover): title-on-color, set in Archivo. */
.card__tick {
  position: absolute;
  top: 13px;
  left: 13px;
  width: 18px;
  height: 2px;
  background: var(--dim);
}
.card__tick--active {
  background: var(--brass);
}
.card__fallback-title {
  position: absolute;
  left: 14px;
  right: 14px;
  top: 50%;
  transform: translateY(-58%);
  font: 800 17px/1.1 var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
}
.card__fallback-author {
  position: absolute;
  left: 14px;
  right: 14px;
  bottom: 14px;
  font: 500 10px var(--font-ui);
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Progress: a brass % pill + a 3px brass seam. Shown only once opened. */
.card__pill {
  position: absolute;
  top: 9px;
  right: 9px;
  padding: 3px 7px;
  border-radius: 999px;
  background: var(--pill-on-cover);
  backdrop-filter: blur(6px);
  border: 1px solid rgba(201, 154, 78, 0.3);
  font: 700 10px var(--font-ui);
  color: var(--brass-bright);
}
.card__continue {
  position: absolute;
  top: 9px;
  left: 9px;
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--brass);
  font: 700 10px var(--font-ui);
  color: var(--on-brass);
}
.card__read {
  position: absolute;
  top: 9px;
  right: 9px;
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--success);
  font: 700 10px var(--font-ui);
  color: #fff;
}
.card__seam {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 3px;
  background: rgba(234, 234, 229, 0.14);
}
:root[data-theme='light'] .card__seam {
  background: rgba(28, 26, 23, 0.18);
}
.card__seam-fill {
  height: 100%;
  background: var(--brass);
}

.card__title {
  margin-top: 9px;
  font: 700 13px var(--font-ui);
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.card__author {
  font: 400 11.5px var(--font-ui);
  color: var(--dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
