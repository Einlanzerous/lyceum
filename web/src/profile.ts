// Local profile identity — just a display name. Lyceum is a single-user,
// self-hosted server with no accounts, so this is a persisted local label
// shown at the top of Settings and used as the library avatar's initial.
// (The old hard-coded "R" avatar was a placeholder for this — "Reader".)

import { computed, ref, watch } from 'vue'

const STORAGE_KEY = 'lyceum.profileName'
const DEFAULT_NAME = 'Reader'

function initialName(): string {
  try {
    return (localStorage.getItem(STORAGE_KEY) ?? '').trim()
  } catch {
    // localStorage unavailable (e.g. private mode).
    return ''
  }
}

// Empty is allowed (unset) — the avatar/label fall back to the default.
const name = ref<string>(initialName())

watch(name, (value) => {
  try {
    localStorage.setItem(STORAGE_KEY, value)
  } catch {
    // best-effort persistence
  }
})

// First letter for the avatar, falling back to the default's initial.
const initial = computed(() => (name.value.trim()[0] ?? DEFAULT_NAME[0]!).toUpperCase())

export function useProfile() {
  return {
    name,
    initial,
    defaultName: DEFAULT_NAME,
    set(value: string): void {
      name.value = value.trim()
    },
  }
}
