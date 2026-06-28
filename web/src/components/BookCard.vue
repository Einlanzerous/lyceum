<script setup lang="ts">
import { computed } from 'vue'
import { coverUrl } from '@/api/client'
import { formatProgress } from '@/api/progress'
import type { Book } from '@/api/types'

const props = defineProps<{ book: Book }>()

// The badge appears only once the book has a known progress value.
const hasProgress = computed(() => typeof props.book.progress === 'number')
const progressLabel = computed(() =>
  hasProgress.value ? formatProgress(props.book.progress as number) : '',
)
const cover = computed(() => (props.book.cover_url ? coverUrl(props.book.id) : ''))
</script>

<template>
  <RouterLink :to="`/reader/${book.id}`" class="book-card" :title="book.title">
    <div class="book-card__cover">
      <img v-if="cover" :src="cover" :alt="`Cover of ${book.title}`" loading="lazy" />
      <div v-else class="book-card__cover--empty">{{ book.title }}</div>
      <span v-if="hasProgress" class="book-card__progress">{{ progressLabel }}</span>
    </div>
    <div class="book-card__meta">
      <span class="book-card__title">{{ book.title }}</span>
      <span class="book-card__author">{{ book.author }}</span>
    </div>
  </RouterLink>
</template>

<style scoped>
.book-card {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  text-decoration: none;
  color: inherit;
}

.book-card__cover {
  position: relative;
  aspect-ratio: 2 / 3;
  border-radius: 6px;
  overflow: hidden;
  background: rgba(0, 0, 0, 0.06);
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.18);
}

.book-card__cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.book-card__cover--empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  padding: 0.5rem;
  text-align: center;
  font-size: 0.85rem;
  color: var(--muted);
}

.book-card__progress {
  position: absolute;
  right: 0.4rem;
  bottom: 0.4rem;
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  font-size: 0.72rem;
  font-weight: 600;
  color: #fff;
  background: rgba(0, 0, 0, 0.66);
}

.book-card__meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.book-card__title {
  font-weight: 600;
  font-size: 0.9rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.book-card__author {
  font-size: 0.8rem;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
