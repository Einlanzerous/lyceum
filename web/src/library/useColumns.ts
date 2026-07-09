// Tracks the library grid's live column count so the inline series drawer can be
// inserted at the end of the active card's row (LYCM-36). The breakpoints mirror
// the .lib__grid media queries in LibraryView.
import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

export function useGridColumns(): Ref<number> {
  const cols = ref(6)

  // matchMedia is absent in some non-browser test environments; fall back to the
  // 6-column default there rather than crashing the component.
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return cols
  }

  const narrow = window.matchMedia('(max-width: 760px)')
  const mid = window.matchMedia('(max-width: 1200px)')

  function update(): void {
    if (narrow.matches) cols.value = 2
    else if (mid.matches) cols.value = 4
    else cols.value = 6
  }

  onMounted(() => {
    update()
    narrow.addEventListener('change', update)
    mid.addEventListener('change', update)
  })
  onBeforeUnmount(() => {
    narrow.removeEventListener('change', update)
    mid.removeEventListener('change', update)
  })

  return cols
}
