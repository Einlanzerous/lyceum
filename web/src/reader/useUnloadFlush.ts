// Flush the latest reading position when the page is being torn down or
// backgrounded. Both events matter: `pagehide` covers tab close / navigation,
// while `visibilitychange -> hidden` covers mobile app-switching and tab
// hiding, where `pagehide`/`unload` may never fire. The flush itself must use a
// keepalive request (see putPositionKeepalive) so it survives teardown.

import { onBeforeUnmount, onMounted } from 'vue'

export function useUnloadFlush(flush: () => void): void {
  function onPageHide(): void {
    flush()
  }
  function onVisibilityChange(): void {
    if (document.visibilityState === 'hidden') flush()
  }

  onMounted(() => {
    window.addEventListener('pagehide', onPageHide)
    document.addEventListener('visibilitychange', onVisibilityChange)
  })
  onBeforeUnmount(() => {
    window.removeEventListener('pagehide', onPageHide)
    document.removeEventListener('visibilitychange', onVisibilityChange)
  })
}
