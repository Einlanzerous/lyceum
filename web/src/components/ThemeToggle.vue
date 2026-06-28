<script setup lang="ts">
// The brass ☾/☀ circle that flips the app theme. Shared by the reader top bar,
// the library top bar, and the settings page so they stay in lockstep.
import { useTheme } from '@/theme'

const { theme, toggle } = useTheme()
</script>

<template>
  <button
    type="button"
    class="theme-toggle"
    :aria-label="theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
    :title="theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
    @click="toggle()"
  >
    {{ theme === 'dark' ? '☾' : '☀' }}
  </button>
</template>

<style scoped>
.theme-toggle {
  width: 40px;
  height: 40px;
  border-radius: 999px;
  border: 1px solid rgba(201, 154, 78, 0.35);
  background: rgba(201, 154, 78, 0.12);
  color: var(--brass-bright);
  font-size: 16px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
