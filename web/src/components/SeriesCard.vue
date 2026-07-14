<script setup lang="ts">
import { computed } from 'vue'
import { coverSrc } from '@/api/coverSrc'
import { formatProgress } from '@/api/progress'
import type { SeriesGroup } from '@/library/series'

// continueBookId is the id of the in-progress volume when this series is the
// pinned "current read": it turns the Continue chip into a direct link into that
// book, rather than opening the drawer.
const props = defineProps<{ series: SeriesGroup; open: boolean; continueBookId?: number | null }>()
defineEmits<{ (e: 'toggle'): void }>()

const count = computed(() => props.series.members.length)
const cover = computed(() =>
  props.series.coverBook.cover_url ? coverSrc(props.series.coverBook.id) : '',
)
const progressPct = computed(() => Math.round(props.series.progress * 100))
const progressLabel = computed(() => formatProgress(props.series.progress))
</script>

<template>
  <div
    class="series"
    :class="{ 'is-open': open }"
    role="button"
    tabindex="0"
    :aria-expanded="open"
    :title="`${series.name} — ${count} books`"
    @click="$emit('toggle')"
    @keydown.enter.prevent="$emit('toggle')"
    @keydown.space.prevent="$emit('toggle')"
  >
    <!-- Fanned stack: two offset layers peek out behind the top cover. -->
    <div class="series__stack">
      <span class="series__layer series__layer--back" aria-hidden="true" />
      <span class="series__layer series__layer--mid" aria-hidden="true" />
      <div class="series__cover" :class="{ 'series__cover--fallback': !cover }">
        <img
          v-if="cover"
          :src="cover"
          :alt="`Cover of ${series.name}`"
          loading="lazy"
          class="series__img"
        />
        <div class="series__hatch" aria-hidden="true" />

        <span class="series__count">◲ {{ count }}</span>
        <RouterLink
          v-if="continueBookId != null"
          :to="`/reader/${continueBookId}`"
          class="series__continue"
          title="Continue reading"
          @click.stop
        >
          ▸ Continue
        </RouterLink>
        <span v-if="!cover" class="series__fallback-title">{{ series.name }}</span>
        <span class="series__open">Open ▾</span>

        <div class="series__seam" aria-hidden="true">
          <div class="series__seam-fill" :style="{ width: progressPct + '%' }" />
        </div>
      </div>
    </div>

    <div class="series__title">{{ series.name }}</div>
    <div class="series__meta">Series · {{ progressLabel }}</div>
  </div>
</template>

<style scoped>
.series {
  display: block;
  width: 100%;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
  font: inherit;
}

/* The stack reserves room for the two offset layers that fan out to the right. */
.series__stack {
  position: relative;
  aspect-ratio: 366 / 600;
}
.series__layer {
  position: absolute;
  inset: 0;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--surface-raised);
  box-shadow: var(--shadow-card);
}
.series__layer--back {
  transform: translate(10px, 7px) rotate(3deg);
  background: var(--panel);
}
.series__layer--mid {
  transform: translate(5px, 3.5px) rotate(1.5deg);
  background: var(--surface-raised);
}

.series__cover {
  position: absolute;
  inset: 0;
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid var(--border);
  background: var(--surface-raised);
  box-shadow: var(--shadow-card);
  transition:
    transform 0.18s ease,
    box-shadow 0.18s ease;
}
.series:hover .series__cover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-pop);
}
/* Open: a 2px brass ring signals the active series (handoff contract). */
.series.is-open .series__cover {
  box-shadow:
    0 0 0 2px var(--brass),
    var(--shadow-pop);
}
.series__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.series__hatch {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: repeating-linear-gradient(135deg, var(--hatch) 0 2px, transparent 2px 9px);
}

.series__count {
  position: absolute;
  top: 10px;
  left: 10px;
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--pill-on-cover);
  backdrop-filter: blur(6px);
  border: 1px solid rgba(201, 154, 78, 0.3);
  font: 700 10px var(--font-ui);
  letter-spacing: 0.04em;
  color: var(--brass-bright);
}
.series__continue {
  position: absolute;
  top: 10px;
  right: 10px;
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--brass);
  font: 700 10px var(--font-ui);
  color: var(--on-brass);
  text-decoration: none;
  cursor: pointer;
}
.series__continue:hover {
  background: var(--brass-bright);
}
.series__fallback-title {
  position: absolute;
  left: 14px;
  right: 22px;
  top: 50%;
  transform: translateY(-58%);
  font: 800 16px/1.06 var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
}
.series__open {
  position: absolute;
  left: 12px;
  bottom: 12px;
  font: 600 9px var(--font-ui);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(234, 234, 229, 0.7);
}
:root[data-theme='light'] .series__open {
  color: rgba(244, 241, 234, 0.75);
}
.series__seam {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 3px;
  background: rgba(234, 234, 229, 0.14);
}
:root[data-theme='light'] .series__seam {
  background: rgba(28, 26, 23, 0.18);
}
.series__seam-fill {
  height: 100%;
  background: var(--brass);
}

.series__title {
  margin-top: 9px;
  font: 700 13px var(--font-ui);
  color: var(--brass);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.series__meta {
  font: 400 11.5px var(--font-ui);
  color: var(--dim);
}
</style>
