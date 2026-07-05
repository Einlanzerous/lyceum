/// <reference types="vite/client" />

// Build target injected by Vite's define (vite.config.ts): 'native' for the
// Wails desktop shell (LYCM-300), absent for the web build. Merges with
// Vite's own ImportMetaEnv.
interface ImportMetaEnv {
  readonly VITE_LYCEUM_TARGET?: string
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}
