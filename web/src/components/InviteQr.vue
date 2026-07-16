<script setup lang="ts">
// The invite as a QR (LYCM-88).
//
// Encodes a `<origin>/sign-in?token=…` URL, not the bare key, so the new device
// can be signed in with nothing but its stock camera app: point, tap the
// notification, land on the sign-in screen already redeeming. That URL round-trip
// is also why this works on Lyceum's plain-HTTP LAN — no getUserMedia, no secure
// context, just navigation.
//
// The QR is rendered to a data-URL on a white quiet-zone tile (QR contrast has to
// survive the app's dark surfaces), so nothing here touches the network.

import { ref, watchEffect } from 'vue'
import QRCode from 'qrcode'
import { inviteSignInUrl } from '@/api/invite'

const props = defineProps<{ token: string }>()

const src = ref('')
const failed = ref(false)

watchEffect(async () => {
  failed.value = false
  try {
    src.value = await QRCode.toDataURL(inviteSignInUrl(window.location.origin, props.token), {
      errorCorrectionLevel: 'M',
      margin: 2,
      width: 240,
      color: { dark: '#000000', light: '#ffffff' },
    })
  } catch {
    // A QR that won't render must not take the reveal down with it — the copyable
    // key beside it is still the source of truth.
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
