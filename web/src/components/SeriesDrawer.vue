<script setup lang="ts">
import { computed } from 'vue'
import { coverUrl } from '@/api/client'
import { formatProgress } from '@/api/progress'
import { memberStatus, resumeIndex, type MemberStatus, type SeriesGroup } from '@/library/series'
import type { Book } from '@/api/types'

const props = defineProps<{ series: SeriesGroup; arrowLeftPct: number }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const resumeAt = computed(() => resumeIndex(props.series.members))
const resumeBook = computed<Book>(() => props.series.members[resumeAt.value]!)

const STATUS_LABEL: Record<MemberStatus, string> = {
  finished: 'Finished',
  'in-progress': 'In progress',
  'not-started': 'Not started',
}

function coverFor(b: Book): string {
  return b.cover_url ? coverUrl(b.id) : ''
}
function pct(b: Book): number {
  return Math.round((b.progress ?? 0) * 100)
}
</script>

<template>
  <div class="drawer">
    <div class="drawer__inner">
      <span class="drawer__notch" :style="{ left: `${arrowLeftPct}%` }" aria-hidden="true" />
      <div class="drawer__panel">
        <header class="drawer__head">
          <div class="drawer__name">{{ series.name }}</div>
          <div class="drawer__sub">{{ series.members.length }} books · {{ series.author }}</div>
          <div class="drawer__spacer" />
          <RouterLink :to="`/reader/${resumeBook.id}`" class="drawer__resume">
            ▸ Resume book {{ resumeAt + 1 }}
          </RouterLink>
          <button
            type="button"
            class="drawer__close"
            aria-label="Collapse series"
            @click="emit('close')"
          >
            ▴
          </button>
        </header>

        <ul class="drawer__grid">
          <li v-for="(member, i) in series.members" :key="member.id" class="drawer__item">
            <RouterLink :to="`/reader/${member.id}`" class="drawer__link" :title="member.title">
              <div class="drawer__cover" :class="{ 'drawer__cover--fallback': !coverFor(member) }">
                <img
                  v-if="coverFor(member)"
                  :src="coverFor(member)"
                  :alt="`Cover of ${member.title}`"
                  loading="lazy"
                  class="drawer__img"
                />
                <span v-else class="drawer__fallback-title">{{ member.title }}</span>

                <span
                  class="drawer__badge"
                  :class="{ 'is-read': memberStatus(member) === 'finished' }"
                  >{{ i + 1 }}</span
                >
                <span v-if="memberStatus(member) === 'in-progress'" class="drawer__pct">{{
                  formatProgress(member.progress ?? 0)
                }}</span>
                <div v-if="pct(member) > 0" class="drawer__seam" aria-hidden="true">
                  <div class="drawer__seam-fill" :style="{ width: pct(member) + '%' }" />
                </div>
              </div>
              <div class="drawer__title">{{ member.title }}</div>
              <div class="drawer__status" :class="`is-${memberStatus(member)}`">
                {{ STATUS_LABEL[memberStatus(member)] }}
              </div>
            </RouterLink>
          </li>
        </ul>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Full-bleed across the grid; the 0fr→1fr row trick animates the reflow. */
.drawer {
  grid-column: 1 / -1;
  display: grid;
  grid-template-rows: 1fr;
  margin-top: 16px;
}
.drawer__inner {
  position: relative;
  min-height: 0;
  overflow: hidden;
}
.drawer__notch {
  position: absolute;
  top: -8px;
  width: 16px;
  height: 16px;
  transform: translateX(-50%) rotate(45deg);
  background: var(--panel);
  border-left: 1px solid rgba(201, 154, 78, 0.35);
  border-top: 1px solid rgba(201, 154, 78, 0.35);
}
.drawer__panel {
  border-radius: 12px;
  background: var(--panel);
  border: 1px solid rgba(201, 154, 78, 0.35);
  padding: 22px 24px 24px;
}

.drawer__head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
}
.drawer__name {
  font: 800 17px var(--font-display);
  color: var(--text);
}
.drawer__sub {
  font: 600 11px var(--font-ui);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--brass-bright);
}
.drawer__spacer {
  flex: 1;
}
.drawer__resume {
  padding: 7px 14px;
  border-radius: 999px;
  border: 1px solid rgba(201, 154, 78, 0.4);
  background: rgba(201, 154, 78, 0.12);
  color: var(--brass-bright);
  font: 700 12px var(--font-ui);
  cursor: pointer;
  white-space: nowrap;
  text-decoration: none;
}
.drawer__resume:hover {
  border-color: var(--brass);
}
.drawer__close {
  width: 32px;
  height: 32px;
  flex: none;
  border-radius: 50%;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--muted);
  font-size: 13px;
  cursor: pointer;
}

.drawer__grid {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 22px;
}
@media (max-width: 1200px) {
  .drawer__grid {
    grid-template-columns: repeat(4, 1fr);
  }
}
@media (max-width: 760px) {
  .drawer__grid {
    grid-template-columns: repeat(3, 1fr);
  }
}
.drawer__link {
  display: block;
  text-decoration: none;
  color: inherit;
}
.drawer__cover {
  position: relative;
  aspect-ratio: 366 / 600;
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid var(--border);
  background: var(--surface-raised);
  box-shadow: var(--shadow-card);
  transition: transform 0.16s ease;
}
.drawer__link:hover .drawer__cover {
  transform: translateY(-3px);
}
.drawer__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.drawer__fallback-title {
  position: absolute;
  left: 12px;
  right: 12px;
  top: 50%;
  transform: translateY(-55%);
  font: 800 15px/1.08 var(--font-display);
  color: var(--text);
}
.drawer__badge {
  position: absolute;
  top: 9px;
  left: 9px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--pill-on-cover);
  border: 1px solid var(--border-strong);
  font: 800 11px var(--font-display);
  color: var(--dim);
}
.drawer__badge.is-read {
  color: var(--brass-bright);
  border-color: rgba(201, 154, 78, 0.3);
}
.drawer__pct {
  position: absolute;
  top: 9px;
  right: 9px;
  padding: 3px 7px;
  border-radius: 999px;
  background: var(--pill-on-cover);
  border: 1px solid rgba(201, 154, 78, 0.3);
  font: 700 10px var(--font-ui);
  color: var(--brass-bright);
}
.drawer__seam {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 3px;
  background: rgba(234, 234, 229, 0.14);
}
:root[data-theme='light'] .drawer__seam {
  background: rgba(28, 26, 23, 0.18);
}
.drawer__seam-fill {
  height: 100%;
  background: var(--brass);
}
.drawer__title {
  margin-top: 8px;
  font: 700 12.5px var(--font-ui);
  color: var(--text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.drawer__status {
  font: 400 11px var(--font-ui);
  color: var(--dim);
}
.drawer__status.is-finished {
  color: var(--brass-bright);
}

/* Open/close animation: height (via 0fr↔1fr) + fade, ~200ms. */
.drawer-enter-active,
.drawer-leave-active {
  transition:
    grid-template-rows 0.2s ease,
    opacity 0.2s ease;
}
.drawer-enter-from,
.drawer-leave-to {
  grid-template-rows: 0fr;
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .drawer-enter-active,
  .drawer-leave-active {
    transition: none;
  }
}
</style>
