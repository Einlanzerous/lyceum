import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The Go backend (Phase 1) exposes no CORS middleware, so in development we proxy
// the API routes to it and the app only ever calls same-origin relative URLs.
// In production LYCM-207 serves this built bundle from the same Go server, so the
// same relative URLs resolve directly — no proxy needed there.
const BACKEND = process.env.LYCEUM_BACKEND ?? 'http://localhost:8080'
const apiRoutes = ['/upload', '/library', '/sync', '/books']

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: Object.fromEntries(
      apiRoutes.map((route) => [route, { target: BACKEND, changeOrigin: true }]),
    ),
  },
})
