// Cover image sources that survive authentication (LYCM-801).
//
// A cover is rendered as a plain `<img :src>`, and an <img> request carries no
// Authorization header. On the **web** build that is fine: the request is
// same-origin, so the browser sends the `lyceum_session` cookie by itself.
//
// The **Wails shell** is the problem. It is served from `wails.localhost` and
// calls a remote backend — a *cross-site* request, on which the browser will not
// send a SameSite=Lax cookie. (SameSite=None would require HTTPS, which a home
// server on a LAN generally isn't.) So in the native shell an <img> has neither
// credential, and every cover on the shelf would 401 the moment the server turns
// enforcement on.
//
// So there, and only there, we fetch the bytes through apiFetch — which *can*
// send the bearer token — and hand the <img> an object URL instead.

import { reactive } from 'vue'
import { isNativeShell } from './base'
import { coverUrl } from './client'
import { apiFetch } from './http'

// A reactive map, so the plain-function call sites below re-render when a fetch
// lands without any of them having to know this is asynchronous.
const objectUrls = reactive(new Map<number, string>())
const inFlight = new Set<number>()

// Cap how many cover fetches are in flight at once (LYCM-96). In the native
// shell every card fetches its own cover bytes through apiFetch as it mounts;
// on a large shelf that fires one request per book the instant the library
// renders, so the whole grid's covers all queue against the connection pool at
// once (over HTTP/1.1 the browser serializes them ~6-at-a-time, which reads as
// "downloading every cover in series"). Bounding concurrency turns that flood
// into ordered waves — the visible covers land first and the rest follow as
// slots free, instead of hundreds of pending fetches and object URLs at once.
// The web build never reaches here (it returns the plain URL and lets the
// <img loading="lazy"> defer off-screen images natively), so this only shapes
// the native path.
export const MAX_CONCURRENT_COVER_FETCHES = 6
let activeFetches = 0
const fetchQueue: Array<() => void> = []

// Run `task` when a concurrency slot is free, releasing the slot (and starting
// the next queued task) when it settles. FIFO, so covers requested first —
// which, as cards mount top-to-bottom, are the ones nearest the top of the
// shelf — load first.
function scheduleCoverFetch(task: () => Promise<void>): void {
  const run = (): void => {
    activeFetches++
    void task().finally(() => {
      activeFetches--
      fetchQueue.shift()?.()
    })
  }
  if (activeFetches < MAX_CONCURRENT_COVER_FETCHES) run()
  else fetchQueue.push(run)
}

/**
 * The `src` for a book's cover.
 *
 * Synchronous on purpose — every call site is a `<img :src="coverSrc(id)">` or a
 * computed over one. On the web it returns the URL immediately. In the native
 * shell it returns '' on the first call and kicks off an authenticated fetch;
 * when that lands the reactive map updates and the caller re-renders with the
 * object URL. An `<img src="">` renders nothing, which is the same thing the
 * components already show for a book with no cover.
 */
export function coverSrc(id: number): string {
  if (!isNativeShell()) return coverUrl(id)

  const cached = objectUrls.get(id)
  if (cached) return cached

  if (!inFlight.has(id)) {
    inFlight.add(id)
    scheduleCoverFetch(async () => {
      try {
        const res = await apiFetch(`/books/${id}/cover`)
        if (res.ok) objectUrls.set(id, URL.createObjectURL(await res.blob()))
      } catch {
        // A cover that won't load is a placeholder, not an error worth surfacing.
      } finally {
        inFlight.delete(id)
      }
    })
  }
  return ''
}

/**
 * Drop a cached cover so the next render re-fetches it — used after a cover is
 * replaced or re-fetched in the review queue, where the bytes change under a
 * stable id.
 */
export function invalidateCover(id: number): void {
  const url = objectUrls.get(id)
  if (url) URL.revokeObjectURL(url)
  objectUrls.delete(id)
}
