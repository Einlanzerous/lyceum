<script setup lang="ts">
// The invite as a QR (LYCM-88).
//
// Encodes a `<origin>/sign-in?token=…` URL, not the bare key, so the new device
// can be signed in with nothing but its stock camera app: point, tap the
// notification, land on the sign-in screen already redeeming. That URL round-trip
// is also why this works on Lyceum's plain-HTTP LAN — no getUserMedia, no secure
// context, just navigation.
//
// `signInUrl` is the server's answer for which origin to encode, and it wins when
// present (LYCM-102). This browser's own origin is the wrong one to assume: where
// the web app sits behind Cloudflare Access, a QR built from `window.location`
// sends the phone to an SSO wall its bearer token cannot open, and the failure
// lands on the person holding the phone rather than on anyone who could debug it.
//
// Falling back, `apiBase()` is the origin to use — not `window.location.origin`.
// They are the same thing in the browser build (same-origin, so apiBase() is ''),
// but in the Wails shell the page is served from `wails.localhost` while the
// backend is a remote server: encoding the shell's own origin would hand out a QR
// that resolves nowhere on any device but this one. apiBase() is already the seam
// that answers "which origin is the backend", so it answers this too.
//
// The QR is rendered to a data-URL on a white quiet-zone tile (QR contrast has to
// survive the app's dark surfaces), so nothing here touches the network.

import { computed, ref, watchEffect } from 'vue'
import QRCode from 'qrcode'
import { inviteSignInUrl } from '@/api/invite'
import { apiBase } from '@/api/base'

const props = defineProps<{ token: string; signInUrl?: string }>()

const target = computed(
  () =>
    props.signInUrl?.trim() || inviteSignInUrl(apiBase() || window.location.origin, props.token),
)

const src = ref('')
const failed = ref(false)

// Guards against a slow render for a spent invite landing after a fast one for
// the invite that replaced it. Re-issues make two renders in flight a real
// sequence — "Add a device" then "lost it, issue another" — and the failure mode
// is the worst kind here: a QR that scans cleanly, beside the key it does not
// match, with nothing on screen to say which one is live.
let renderSeq = 0

watchEffect(async () => {
  const seq = ++renderSeq
  // Read synchronously so Vue tracks the dependency before the first await.
  const url = target.value
  failed.value = false
  try {
    const rendered = await QRCode.toDataURL(url, {
      errorCorrectionLevel: 'M',
      margin: 2,
      width: 240,
      color: { dark: '#000000', light: '#ffffff' },
    })
    if (seq !== renderSeq) return
    src.value = rendered
  } catch {
    // A QR that won't render must not take the reveal down with it — the copyable
    // key beside it is still the source of truth.
    if (seq !== renderSeq) return
    src.value = ''
    failed.value = true
  }
})
</script>

<template>
  <figure v-if="!failed" class="qr">
    <div class="qr__tile">
      <img v-if="src" class="qr__img" :src="src" alt="Sign-in QR code" width="240" height="240" />
    </div>
    <figcaption class="qr__cap">Or scan with your phone's camera to sign in</figcaption>
  </figure>
</template>

<style scoped>
.qr {
  margin: 18px 0 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 9px;
}
.qr__tile {
  padding: 12px;
  background: #fff;
  border-radius: 12px;
  border: 1px solid var(--border-strong);
  line-height: 0;
}
.qr__img {
  width: 200px;
  height: 200px;
  display: block;
}
.qr__cap {
  font: 500 11.5px var(--font-ui);
  color: var(--dim);
  text-align: center;
}
</style>
