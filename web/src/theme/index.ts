// App-wide theme: dark (the design default) or light "warm paper". A single
// reactive source drives the chrome (via the data-theme attribute + CSS
// variables) and the epub.js reading surface (useReader watches it), so the
// reader's toggle re-themes everything at once. Persisted across reloads.

import { ref, watch } from 'vue'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'lyceum.theme'

function initialTheme(): Theme {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'light' || saved === 'dark') return saved
  } catch {
    // localStorage unavailable (e.g. private mode): fall through to default.
  }
  return 'dark'
}

const theme = ref<Theme>(initialTheme())

function apply(value: Theme): void {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-theme', value)
  }
  try {
    localStorage.setItem(STORAGE_KEY, value)
  } catch {
    // best-effort persistence
  }
}

apply(theme.value)
watch(theme, apply)

export function useTheme() {
  return {
    theme,
    toggle(): void {
      theme.value = theme.value === 'dark' ? 'light' : 'dark'
    },
    set(value: Theme): void {
      theme.value = value
    },
  }
}
