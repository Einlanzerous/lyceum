<script setup lang="ts">
// Root shell. The header is hidden on the reader route so the reading surface
// gets the full viewport.
import { computed } from 'vue'
import { useRoute, RouterView, RouterLink } from 'vue-router'

const route = useRoute()
const chromeless = computed(() => route.meta.chromeless === true)
</script>

<template>
  <header v-if="!chromeless" class="app-header">
    <RouterLink to="/" class="app-title">Lyceum</RouterLink>
  </header>
  <main class="app-main" :class="{ 'app-main--chromeless': chromeless }">
    <RouterView />
  </main>
</template>
