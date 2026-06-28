<script setup lang="ts">
// Root shell. Both routes own their full chrome (the library has a floating top
// bar; the reader is edge-to-edge), so App is just the themed viewport.
import { RouterView } from 'vue-router'
</script>

<template>
  <main class="app-main">
    <RouterView />
  </main>
</template>
