<script setup lang="ts">
// The front door (LYCM-801, design frames 22 & 23).
//
// One field: the invite. No passwords exist in Lyceum, so there is nothing else
// to ask for — and the device label is inferred rather than requested, keeping
// the door a single field while still filling the devices list with something
// recognisable.
//
// The one subtlety worth knowing: a wrong, spent, and expired invite all come
// back as the same 401. The server genuinely cannot tell them apart (that is
// deliberate — it stops an attacker probing which invites exist), so the copy
// names all three possibilities rather than guessing at one and misleading.

import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { inferDeviceLabel } from '@/api/device'
import { hasBackend, isNativeShell } from '@/api/base'
import { peekLegacyProfileName } from '@/profile'
import { useAuthStore } from '@/stores/auth'
import ServerSettings from '@/components/ServerSettings.vue'

const router = useRouter()
const auth = useAuthStore()

const token = ref('')
const deviceLabel = ref(inferDeviceLabel())
const editingDevice = ref(false)
const submitting = ref(false)

type Failure = { kind: 'rejected' } | { kind: 'unreachable' } | null
const failure = ref<Failure>(null)
const showServer = ref(false)

// Someone who has been reading for months already has a name in this browser.
// Greeting them by it is the difference between "sign in to this app you've
// never seen" and "keep the library you already have".
const returningName = ref('')
onMounted(() => {
  returningName.value = peekLegacyProfileName()
})
const isUpgrade = computed(() => returningName.value !== '')

const canSubmit = computed(() => token.value.trim().length > 0 && !submitting.value)
const initial = computed(() => (returningName.value.trim()[0] ?? 'R').toUpperCase())

async function submit(): Promise<void> {
  if (!canSubmit.value) return
  submitting.value = true
  failure.value = null
  try {
    await auth.signIn(token.value, deviceLabel.value)
    await router.replace('/')
  } catch (err) {
    // A 401 is the invite being bad. Anything else — most often a dead server on
    // the native path — is not the person's fault and must not wear the red
    // "bad key" banner.
    failure.value =
      err instanceof ApiError && err.status === 401 ? { kind: 'rejected' } : { kind: 'unreachable' }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section class="door">
    <div class="door__card" :class="{ 'is-bad': failure?.kind === 'rejected' }">
      <div class="brand">
        <span class="brand__mark" aria-hidden="true"></span>
        <span class="brand__word">LYCEUM</span>
      </div>

      <!-- Rejected: the banner replaces the headline, so the first thing read is
           what went wrong, not a greeting. -->
      <div v-if="failure?.kind === 'rejected'" class="alert" role="alert">
        <div class="alert__title">That key didn't work.</div>
        <p class="alert__body">
          Invites work once and expire after 7 days — this one may be spent, expired, or mistyped.
          We can't tell which. Ask for a fresh invite, or check you copied the whole thing.
        </p>
      </div>

      <template v-else-if="isUpgrade">
        <div class="upgrade">
          <div class="upgrade__avatar" aria-hidden="true">{{ initial }}</div>
          <div>
            <div class="eyebrow">Welcome back</div>
            <h1 class="door__title door__title--upgrade">
              You've been reading as {{ returningName }}.
            </h1>
          </div>
        </div>
        <p class="door__lede">
          This library just turned on accounts. Sign in once on this device to keep going —
          <b>your shelf and your place in every book come with you.</b>
        </p>
        <ul class="promises">
          <li>
            <span class="tick" aria-hidden="true">✓</span>
            “{{ returningName }}” becomes your account name — you can change it any time.
          </li>
          <li>
            <span class="tick" aria-hidden="true">✓</span>
            Every bookmark and reading position stays exactly where it is.
          </li>
        </ul>
      </template>

      <template v-else>
        <div class="eyebrow eyebrow--center">The reading room</div>
        <h1 class="door__title">You've been handed a key.</h1>
        <p class="door__lede door__lede--center">
          Paste the invite a housemate gave you — or the one printed in the server log.
        </p>
      </template>

      <form @submit.prevent="submit">
        <label class="field-label" for="invite">
          {{ isUpgrade && !failure ? 'Paste your invite to continue' : 'Invite key' }}
        </label>
        <div
          class="key"
          :class="{ 'is-filled': token.trim(), 'is-bad': failure?.kind === 'rejected' }"
        >
          <input
            id="invite"
            v-model="token"
            class="key__input"
            type="text"
            placeholder="lyc_…"
            autocomplete="off"
            autocapitalize="off"
            autocorrect="off"
            spellcheck="false"
            :disabled="submitting"
            @input="failure = null"
          />
          <span v-if="token.trim() && !failure" class="key__ok" aria-hidden="true">✓</span>
        </div>

        <!-- Inferred, shown, correctable — never a second required field. -->
        <div v-if="token.trim() && !failure" class="device">
          <span aria-hidden="true">🖥</span>
          <template v-if="!editingDevice">
            This device · <b>{{ deviceLabel }}</b> ·
            <button type="button" class="linkish" @click="editingDevice = true">change</button>
          </template>
          <input
            v-else
            v-model="deviceLabel"
            class="device__input"
            type="text"
            maxlength="40"
            aria-label="Device name"
            @blur="editingDevice = false"
            @keyup.enter="editingDevice = false"
          />
        </div>

        <button type="submit" class="unlock" :disabled="!canSubmit">
          <span v-if="submitting" class="spinner" aria-hidden="true"></span>
          {{
            submitting
              ? 'Unlocking…'
              : failure?.kind === 'rejected'
                ? 'Try again'
                : isUpgrade
                  ? 'Keep my library'
                  : 'Unlock the library'
          }}
        </button>
      </form>

      <p v-if="failure?.kind === 'rejected'" class="foot">
        Still stuck? Whoever runs the library can issue another, or run
        <code>lyceum mint-token</code> on the box.
      </p>
      <p v-else-if="isUpgrade" class="foot">
        Owner? Your invite is in the server log on first boot. Everyone else: ask the owner.
      </p>
      <p v-else-if="token.trim()" class="foot">
        Whitespace and line breaks are fine — we clean it up.
      </p>
      <p v-else class="foot">
        No invite? Ask whoever runs this library for one. There are no passwords here.
      </p>
    </div>

    <!-- Unreachable is a different failure with a different shape: nothing is
         wrong with the person's key, so it gets its own calm panel and a way to
         fix what actually broke. -->
    <div v-if="failure?.kind === 'unreachable'" class="offline">
      <div class="offline__row">
        <span class="offline__icon" aria-hidden="true">⚲</span>
        <div>
          <div class="offline__title">Can't reach this library.</div>
          <div class="offline__body">
            The server may be off, or this device isn't on the same network.
          </div>
        </div>
      </div>
      <div class="offline__actions">
        <button type="button" class="btn btn--brass" :disabled="submitting" @click="submit">
          Retry
        </button>
        <button
          v-if="isNativeShell()"
          type="button"
          class="btn btn--ghost"
          @click="showServer = !showServer"
        >
          Server address
        </button>
      </div>
      <div v-if="showServer" class="offline__server"><ServerSettings /></div>
    </div>

    <!-- A native shell that has never been pointed anywhere can't sign in at all. -->
    <div v-else-if="isNativeShell() && !hasBackend()" class="offline">
      <div class="offline__title">Point this app at your library first.</div>
      <div class="offline__server"><ServerSettings /></div>
    </div>
  </section>
</template>

<style scoped>
.door {
  min-height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 20px;
  padding: 48px 20px;
}

.door__card {
  width: 100%;
  max-width: 420px;
  padding: 36px 32px;
  border-radius: 16px;
  background: var(--surface-raised);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-pop);
}
.door__card.is-bad {
  border-color: color-mix(in srgb, var(--error) 34%, transparent);
}

.brand {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
}
.brand__mark {
  width: 15px;
  height: 15px;
  border-radius: 3px;
  border: 2px solid var(--brass);
  transform: rotate(45deg);
}
.brand__word {
  font: 800 15px var(--font-display);
  letter-spacing: 0.18em;
  color: var(--text);
}

.eyebrow {
  font: 700 11px var(--font-display);
  letter-spacing: 0.2em;
  color: var(--brass);
  text-transform: uppercase;
}
.eyebrow--center {
  text-align: center;
  margin-top: 26px;
}

.door__title {
  font: 800 26px/1.1 var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
  text-align: center;
  margin: 10px 0 0;
}
.door__title--upgrade {
  font-size: 23px;
  text-align: left;
  margin: 2px 0 0;
}

.door__lede {
  font: 400 13.5px/1.55 var(--font-ui);
  color: var(--muted);
  margin: 14px 0 24px;
}
.door__lede--center {
  text-align: center;
}
.door__lede b {
  color: var(--text);
}

.upgrade {
  display: flex;
  align-items: center;
  gap: 13px;
  margin-top: 24px;
}
.upgrade__avatar {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  flex: none;
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
  color: var(--on-brass);
  font: 700 19px var(--font-display);
  display: flex;
  align-items: center;
  justify-content: center;
}

.promises {
  list-style: none;
  margin: 0 0 22px;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.promises li {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 12px 15px;
  border-radius: 11px;
  background: var(--hatch);
  font: 500 13px/1.45 var(--font-ui);
  color: var(--reading);
}
.tick {
  color: var(--success);
  font-size: 15px;
}

.alert {
  margin-top: 24px;
  padding: 13px 15px;
  border-radius: 11px;
  background: color-mix(in srgb, var(--error) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--error) 35%, transparent);
}
.alert__title {
  font: 800 13px var(--font-ui);
  color: var(--error);
  margin-bottom: 4px;
}
.alert__body {
  font: 400 12.5px/1.55 var(--font-ui);
  color: color-mix(in srgb, var(--error) 55%, var(--text));
  margin: 0;
}

.field-label {
  display: block;
  font: 600 10px var(--font-ui);
  letter-spacing: 0.12em;
  color: var(--dim);
  text-transform: uppercase;
  margin: 20px 0 7px;
}

.key {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 13px 15px;
  border-radius: 11px;
  background: var(--panel);
  border: 1px solid var(--border-strong);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.key.is-filled {
  border-color: color-mix(in srgb, var(--brass) 55%, transparent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--brass) 12%, transparent);
}
.key.is-bad {
  border-color: color-mix(in srgb, var(--error) 55%, transparent);
  box-shadow: none;
}
.key__input {
  flex: 1;
  min-width: 0;
  border: 0;
  background: transparent;
  color: var(--text);
  font:
    400 14px ui-monospace,
    monospace;
  outline: none;
}
.key__input::placeholder {
  color: var(--dimmer);
}
.key__ok {
  color: var(--success);
  font-size: 13px;
}

.device {
  margin-top: 9px;
  display: flex;
  align-items: center;
  gap: 7px;
  font: 500 11.5px var(--font-ui);
  color: var(--dim);
}
.device b {
  color: var(--reading);
  font-weight: 700;
}
.device__input {
  flex: 1;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  background: var(--panel);
  color: var(--text);
  font: 600 11.5px var(--font-ui);
  padding: 5px 8px;
  outline: none;
}
.linkish {
  border: 0;
  background: none;
  padding: 0;
  color: var(--brass);
  font: inherit;
  cursor: pointer;
  text-decoration: none;
}
.linkish:hover {
  text-decoration: underline;
}

.unlock {
  width: 100%;
  margin-top: 16px;
  padding: 14px 0;
  border-radius: 11px;
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font: 800 14px var(--font-ui);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
}
.unlock:disabled {
  background: color-mix(in srgb, var(--brass) 22%, transparent);
  color: color-mix(in srgb, var(--brass) 70%, var(--dim));
  cursor: not-allowed;
}

.spinner {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 2px solid color-mix(in srgb, var(--on-brass) 35%, transparent);
  border-top-color: var(--on-brass);
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.foot {
  margin: 14px 0 0;
  text-align: center;
  font: 400 12px/1.5 var(--font-ui);
  color: var(--dim);
}
.foot code {
  font-family: ui-monospace, monospace;
  color: var(--muted);
}

.offline {
  width: 100%;
  max-width: 420px;
  padding: 24px 26px;
  border-radius: 16px;
  background: var(--surface-raised);
  border: 1px solid var(--border);
}
.offline__row {
  display: flex;
  align-items: center;
  gap: 11px;
}
.offline__icon {
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: 11px;
  background: var(--hatch);
  border: 1px solid var(--border-strong);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--dim);
}
.offline__title {
  font: 800 14px var(--font-ui);
  color: var(--text);
}
.offline__body {
  font: 400 12.5px/1.5 var(--font-ui);
  color: var(--muted);
  margin-top: 2px;
}
.offline__actions {
  margin-top: 16px;
  display: flex;
  gap: 10px;
}
.offline__server {
  margin-top: 16px;
}

.btn {
  flex: 1;
  padding: 11px 0;
  border-radius: 10px;
  font: 700 13px var(--font-ui);
  cursor: pointer;
}
.btn--brass {
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font-weight: 800;
}
.btn--ghost {
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--reading);
}
</style>
