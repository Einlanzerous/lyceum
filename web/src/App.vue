<script setup lang="ts">
// Root shell. Both routes own their full chrome (the library has a floating top
// bar; the reader is edge-to-edge), so App is just the themed viewport — plus
// the native-shell update banner, which checks for a newer release on mount and
// is a no-op in the web build.
import { onMounted } from 'vue'
import { RouterView } from 'vue-router'
import UpdateBanner from '@/components/UpdateBanner.vue'
import SessionEndedDialog from '@/components/SessionEndedDialog.vue'
import { checkForUpdate } from '@/update/useUpdate'

onMounted(() => {
  void checkForUpdate()
})
</script>

<template>
  <main class="app-main">
    <UpdateBanner />
    <RouterView />
    <!-- A 401 can land mid-chapter. Global, so it lands over whatever you were
         doing rather than teleporting you away from it. -->
    <SessionEndedDialog />
  </main>
</template>
