// Reactive wrapper over api/base's server-URL storage (LYCM-300). The native
// shells (Wails/Capacitor) must be pointed at a remote backend; this exposes
// that choice as a Vue ref shared between the first-run prompt (LibraryView)
// and Settings, so saving in one place updates the other and the library
// reloads. In the web build isNative is false and nothing here is shown.

import { ref } from 'vue'
import { getServerUrl, isNativeShell, setServerUrl } from './base'

const server = ref(getServerUrl())

export function useServer() {
  return {
    /** The active backend URL ('' when unconfigured). Reactive. */
    server,
    /** True only in the native shells, where a backend must be configured. */
    isNative: isNativeShell(),
    /** Persist a new backend URL (normalized) and update the shared ref. */
    save(url: string): void {
      setServerUrl(url)
      server.value = getServerUrl()
    },
  }
}
