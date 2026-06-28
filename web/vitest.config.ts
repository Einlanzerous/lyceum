import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// epub.js renders into a real iframe and is not meaningfully testable under
// jsdom; those paths are covered by manual/Playwright smoke (see LYCM-204/205).
// jsdom here is for the pure logic: API client, device_id, progress math, and
// the library component with a mocked client.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.ts'],
  },
})
