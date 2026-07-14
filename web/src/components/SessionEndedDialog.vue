<script setup lang="ts">
// "You've been signed out" (LYCM-801).
//
// A 401 can land mid-chapter — a session revoked from another device, or an
// account removed by the owner. The failure mode to avoid is a reader that blanks
// out, throws, or silently teleports someone to a login screen with their page
// lost. So: a calm sheet over the frozen page, and the first thing it says is
// that nothing was lost. The position was already synced on the last page turn.
//
// The server cannot distinguish "expired" from "removed" for us — both are just a
// token that stops resolving — so `removed` is inferred (we were holding no token
// at all). The copy for each is written to be true regardless.

import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

async function signInAgain(): Promise<void> {
  auth.clearEnded()
  await router.push('/sign-in')
}
</script>

<template>
  <div v-if="auth.endedReason" class="scrim" role="dialog" aria-modal="true">
    <div class="sheet">
      <div class="sheet__icon" aria-hidden="true">🔒</div>

      <template v-if="auth.endedReason === 'expired'">
        <h2 class="sheet__title">You've been signed out.</h2>
        <p class="sheet__body">
          <b>Your place is saved.</b> Sign back in on this device to pick up exactly where you left
          off — nothing was lost. This can happen if your session expired, or was signed out from
          another device.
        </p>
      </template>

      <template v-else>
        <h2 class="sheet__title">This device was signed out.</h2>
        <p class="sheet__body">
          The library owner removed this account. Your reading positions were cleared, but the
          shared shelf is unaffected. To read again, ask the owner for a new invite.
        </p>
      </template>

      <button type="button" class="sheet__btn" @click="signInAgain">Sign in</button>
    </div>
  </div>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  /* Above the reader chrome: this must be the only thing you can act on. */
  z-index: 80;
  background: rgba(8, 8, 7, 0.78);
  backdrop-filter: blur(3px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.sheet {
  width: 420px;
  max-width: 100%;
  padding: 30px 30px 26px;
  border-radius: 18px;
  background: var(--surface-raised);
  border: 1px solid var(--border-strong);
  box-shadow: var(--shadow-pop);
  text-align: center;
}
.sheet__icon {
  width: 46px;
  height: 46px;
  margin: 0 auto;
  border-radius: 13px;
  background: var(--hatch);
  border: 1px solid var(--border-strong);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
}
.sheet__title {
  font: 800 20px var(--font-display);
  color: var(--text);
  margin: 18px 0 8px;
}
.sheet__body {
  font: 400 13.5px/1.6 var(--font-ui);
  color: var(--muted);
  margin: 0;
}
.sheet__body b {
  color: var(--text);
}
.sheet__btn {
  width: 100%;
  margin-top: 22px;
  padding: 13px 0;
  border-radius: 11px;
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font: 800 14px var(--font-ui);
  cursor: pointer;
}
</style>
