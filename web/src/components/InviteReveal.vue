<script setup lang="ts">
// The invite reveal (LYCM-801, design frame 24) — the hero of this feature.
//
// A secret shown exactly once. The server hashes it before storing, so it is not
// recoverable by anyone, including the owner who just created it: close this
// dialog without copying and the only way out is to issue another. That is the
// whole reason this screen earns a modal, a warning, and real copy feedback
// instead of being a row in a list.
//
// It is a key, not a link: single-use, one device, expiring in 7 days.

import { ref } from 'vue'
import type { Invite } from '@/api/auth'
import InviteQr from './InviteQr.vue'

const props = defineProps<{
  /** The freshly minted invite, or null once it has been let go. */
  invite: Invite | null
  /** Set when the person dismissed a reveal without copying — the recovery path. */
  lost: { name: string; userId: number } | null
  reissuing?: boolean
}>()

const emit = defineEmits<{
  /**
   * saved=true  — they copied it, or told us they have it. Just close.
   * saved=false — they walked away (the ✕). The plaintext is now unrecoverable,
   *               so hand over to the "that invite is gone" recovery path rather
   *               than letting a key evaporate silently.
   */
  close: [saved: boolean]
  reissue: [userId: number]
}>()

const copied = ref(false)
const copyFailed = ref(false)

async function copy(): Promise<void> {
  if (!props.invite) return
  try {
    await navigator.clipboard.writeText(props.invite.invite_token)
    copied.value = true
    copyFailed.value = false
  } catch {
    // Clipboard access is denied in insecure contexts — which is exactly where
    // Lyceum lives (a LAN server over plain HTTP). Don't claim success: tell them
    // to select it by hand, because a silently-failed copy here means a lost key.
    copyFailed.value = true
  }
}

async function copyAndClose(): Promise<void> {
  await copy()
  // If the clipboard was blocked (Lyceum on a plain-HTTP LAN is an insecure
  // context, where it usually is), closing now would destroy the only copy of the
  // key. Stay open and let them select it by hand.
  if (copyFailed.value) return
  emit('close', true)
}
</script>

<template>
  <div v-if="invite || lost" class="scrim" role="dialog" aria-modal="true">
    <!-- The reveal -->
    <div v-if="invite" class="sheet" :class="{ 'is-copied': copied }">
      <header class="sheet__head">
        <div class="who">
          <div class="who__avatar" aria-hidden="true">
            {{ (invite.user.display_name[0] ?? '?').toUpperCase() }}
          </div>
          <div>
            <div class="eyebrow">Invite created</div>
            <div class="who__name">A key for {{ invite.user.display_name }}</div>
          </div>
        </div>
        <button type="button" class="x" aria-label="Close" @click="emit('close', false)">✕</button>
      </header>

      <div class="sheet__body">
        <p class="lede">
          Hand this key to {{ invite.user.display_name }}. When they paste it on their device,
          they're in. It's the only credential — treat it like a house key, not a link.
        </p>

        <div class="label">The invite key</div>
        <div class="secret">
          <code class="secret__value" :class="{ 'is-copied': copied }">{{
            invite.invite_token
          }}</code>
          <button type="button" class="secret__copy" :class="{ 'is-copied': copied }" @click="copy">
            <span aria-hidden="true">{{ copied ? '✓' : '⧉' }}</span>
            {{ copied ? 'Copied' : 'Copy key' }}
          </button>
        </div>

        <div class="meta">
          <span><i aria-hidden="true"></i>Expires in 7 days</span>
          <span><i aria-hidden="true"></i>Works once</span>
          <span><i aria-hidden="true"></i>One device</span>
        </div>

        <InviteQr :token="invite.invite_token" />

        <div v-if="copyFailed" class="warn warn--soft">
          Couldn't reach the clipboard — this browser blocks it on an insecure origin. Select the
          key above and copy it by hand.
        </div>

        <div v-else-if="copied" class="done">
          <span class="done__dot" aria-hidden="true"></span>
          Copied to clipboard — now hand it to {{ invite.user.display_name }}. Closing this dialog
          is safe once you've sent it.
        </div>

        <div v-else class="warn">
          <span class="warn__icon" aria-hidden="true">⚠</span>
          <div>
            <b>This is the only time you'll see this key.</b> Copy it before you close — we can't
            show it again. Lost it? Just issue {{ invite.user.display_name }} another.
          </div>
        </div>

        <div class="actions">
          <button type="button" class="btn btn--brass" @click="copyAndClose">
            Copy &amp; close
          </button>
          <button type="button" class="btn btn--ghost" @click="emit('close', true)">
            I've saved it
          </button>
        </div>
      </div>
    </div>

    <!-- Dismissed without copying: the key is genuinely unrecoverable, so the only
         honest thing to offer is another one. -->
    <div v-else-if="lost" class="sheet sheet--lost">
      <div class="sheet__body">
        <div class="lost__head">
          <span aria-hidden="true">🔒</span>
          <div class="lost__title">That invite is gone</div>
        </div>
        <div class="redacted">lyc_•••••••••••••••••••••••</div>
        <p class="lede">
          For security, an invite is shown only once and we can't display it again.
          {{ lost.name }}'s account is still here and waiting — issue a fresh key whenever you're
          ready.
        </p>
        <div class="actions">
          <button
            type="button"
            class="btn btn--brass"
            :disabled="reissuing"
            @click="emit('reissue', lost.userId)"
          >
            {{ reissuing ? 'Issuing…' : `Issue another invite for ${lost.name}` }}
          </button>
          <button type="button" class="btn btn--ghost" @click="emit('close', true)">Not now</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(8, 8, 7, 0.72);
  backdrop-filter: blur(2px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.sheet {
  width: 560px;
  max-width: 100%;
  border-radius: 20px;
  background: var(--surface-raised);
  border: 1px solid color-mix(in srgb, var(--brass) 28%, transparent);
  box-shadow: var(--shadow-pop);
  overflow: hidden;
}
.sheet--lost {
  width: 460px;
  border-color: var(--border);
}
.sheet.is-copied {
  border-color: color-mix(in srgb, var(--success) 32%, transparent);
}

.sheet__head {
  padding: 26px 34px 22px;
  border-bottom: 1px solid var(--border);
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--brass) 14%, transparent),
    transparent
  );
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.who {
  display: flex;
  align-items: center;
  gap: 11px;
}
.who__avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  flex: none;
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
  color: var(--on-brass);
  font: 700 16px var(--font-display);
  display: flex;
  align-items: center;
  justify-content: center;
}
.who__name {
  font: 800 22px var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
}
.eyebrow {
  font: 700 11px var(--font-display);
  letter-spacing: 0.2em;
  color: var(--brass);
  text-transform: uppercase;
}
.x {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: none;
  background: var(--hatch);
  color: var(--muted);
  font-size: 17px;
  cursor: pointer;
}

.sheet__body {
  padding: 28px 34px 32px;
}
.lede {
  font: 400 14px/1.6 var(--font-ui);
  color: var(--reading);
  margin: 0 0 20px;
}

.label {
  font: 600 10px var(--font-ui);
  letter-spacing: 0.14em;
  color: var(--dim);
  text-transform: uppercase;
  margin-bottom: 9px;
}

.secret {
  display: flex;
  align-items: stretch;
  gap: 10px;
}
.secret__value {
  flex: 1;
  padding: 16px 18px;
  border-radius: 12px;
  background: var(--bg);
  border: 1px solid color-mix(in srgb, var(--brass) 40%, transparent);
  font:
    500 15px/1.4 ui-monospace,
    monospace;
  color: var(--brass-bright);
  word-break: break-all;
  user-select: all;
}
.secret__value.is-copied {
  border-color: color-mix(in srgb, var(--success) 35%, transparent);
  color: color-mix(in srgb, var(--success) 75%, var(--text));
}
.secret__copy {
  flex: none;
  width: 120px;
  border-radius: 12px;
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font: 800 14px var(--font-ui);
  cursor: pointer;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
}
.secret__copy.is-copied {
  background: color-mix(in srgb, var(--success) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--success) 40%, transparent);
  color: var(--success);
}

.meta {
  display: flex;
  align-items: center;
  gap: 18px;
  margin-top: 14px;
  flex-wrap: wrap;
}
.meta span {
  display: flex;
  align-items: center;
  gap: 7px;
  font: 600 12.5px var(--font-ui);
  color: var(--muted);
}
.meta i {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--brass);
}

.warn {
  margin-top: 22px;
  padding: 15px 17px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--error) 9%, transparent);
  border: 1px solid color-mix(in srgb, var(--error) 32%, transparent);
  display: flex;
  gap: 12px;
  font: 400 13px/1.55 var(--font-ui);
  color: color-mix(in srgb, var(--error) 45%, var(--text));
}
.warn b {
  color: var(--error);
}
.warn--soft {
  display: block;
}
.warn__icon {
  color: var(--error);
  font-size: 17px;
  line-height: 1.3;
}

.done {
  margin-top: 22px;
  padding: 15px 17px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--success) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--success) 32%, transparent);
  font: 400 13px/1.55 var(--font-ui);
  color: var(--reading);
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.done__dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--success);
  flex: none;
  margin-top: 5px;
}

.lost__head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.lost__title {
  font: 800 17px var(--font-display);
  color: var(--text);
}
.redacted {
  padding: 14px 16px;
  border-radius: 12px;
  background: var(--bg);
  border: 1px dashed var(--border-strong);
  font:
    500 14.5px ui-monospace,
    monospace;
  color: var(--dimmer);
  letter-spacing: 0.04em;
  margin-bottom: 14px;
}

.actions {
  margin-top: 24px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.btn {
  padding: 14px 22px;
  border-radius: 12px;
  font: 700 14px var(--font-ui);
  cursor: pointer;
}
.btn--brass {
  flex: 1;
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font-weight: 800;
}
.btn--brass:disabled {
  opacity: 0.6;
  cursor: progress;
}
.btn--ghost {
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--reading);
}
</style>
