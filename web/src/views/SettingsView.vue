<script setup lang="ts">
// Preferences home (LYCM-501). Theme (LYCM-501) + opt-in reading font
// (LYCM-502). Both write to persisted reactive stores the reader watches, so a
// change here re-renders an open book live.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useTheme, type Theme } from '@/theme'
import { useReadingFont } from '@/reader/readingFont'
import { READING_FONTS, resolveFontFamily } from '@/reader/font'
import { isNativeShell } from '@/api/base'
import { listDevices, revokeDevice, type Device } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import ServerSettings from '@/components/ServerSettings.vue'

const router = useRouter()
const { theme, set } = useTheme()
const { font, set: setFont } = useReadingFont()

// Identity is the account now, not a label in this browser (LYCM-801). The name
// lives on the server and follows the person to every device they read on.
const auth = useAuthStore()

const editingName = ref(false)
const draftName = ref('')
const savingName = ref(false)

function startRename(): void {
  draftName.value = auth.displayName
  editingName.value = true
}

async function saveName(): Promise<void> {
  const next = draftName.value.trim()
  if (!next || next === auth.displayName) {
    editingName.value = false
    return
  }
  savingName.value = true
  try {
    await auth.rename(next)
    editingName.value = false
  } finally {
    savingName.value = false
  }
}

// Your devices. A session never expires, so a device you lost or lent stays
// signed in until you come here and cut it off — which is the only reason this
// list exists, and why the current one is marked rather than hidden.
const devices = ref<Device[]>([])
const devicesLoading = ref(true)

async function loadDevices(): Promise<void> {
  devicesLoading.value = true
  try {
    devices.value = await listDevices()
  } catch {
    devices.value = []
  } finally {
    devicesLoading.value = false
  }
}
onMounted(loadDevices)

async function revoke(d: Device): Promise<void> {
  await revokeDevice(d.id)
  // Revoking the device you are *on* is a sign-out; the 401 handler takes it from
  // here, so don't fight it by reloading a list we can no longer read.
  if (d.current) {
    await auth.signOut()
    await router.push('/sign-in')
    return
  }
  await loadDevices()
}

const signingOut = ref(false)
async function signOut(): Promise<void> {
  signingOut.value = true
  try {
    await auth.signOut()
    await router.push('/sign-in')
  } finally {
    signingOut.value = false
  }
}

function seenAt(iso: string | null): string {
  if (!iso) return 'not used yet'
  const hours = Math.floor((Date.now() - new Date(iso).getTime()) / 3_600_000)
  if (hours < 1) return 'active now'
  if (hours < 24) return 'last used today'
  const days = Math.floor(hours / 24)
  return days === 1 ? 'last used yesterday' : `last used ${days} days ago`
}

// Native shells (Wails/Capacitor) talk to a remote backend the user configures;
// the web build is served by that backend, so the section is hidden there.
const isNative = isNativeShell()

const themeOptions: { value: Theme; label: string }[] = [
  { value: 'dark', label: 'Dark' },
  { value: 'light', label: 'Light' },
]

// Preview the chosen face. "Publisher" has no stack of its own, so fall back to
// the design's default reading serif as a stand-in for typical book typography.
const specimenFamily = computed(() => resolveFontFamily(font.value) ?? 'var(--font-read)')
</script>

<template>
  <section class="settings">
    <header class="settings__bar">
      <button type="button" class="pill" @click="router.push('/')">
        <span>←</span><span>Library</span>
      </button>
    </header>

    <div class="settings__body">
      <div class="settings__eyebrow">Preferences</div>
      <h1 class="settings__title">Settings</h1>

      <!-- Account (LYCM-801) — the real one. Replaces the local-only "Profile". -->
      <div class="group">
        <div class="group__label">Account</div>
        <div class="card">
          <!-- Identity -->
          <div class="row row--identity">
            <div class="acct__avatar" aria-hidden="true">{{ auth.initial }}</div>
            <div class="acct__who">
              <div class="acct__name">
                <span>{{ auth.displayName }}</span>
                <span v-if="auth.isOwner" class="badge">Owner</span>
              </div>
              <div class="acct__email">{{ auth.user?.email }}</div>
            </div>
            <button v-if="!editingName" type="button" class="btn btn--ghost" @click="startRename">
              ✎ Edit name
            </button>
          </div>

          <!-- Inline rename -->
          <div v-if="editingName" class="row row--editing">
            <div class="edit">
              <div class="edit__label">Display name — editing</div>
              <input
                v-model="draftName"
                class="edit__input"
                type="text"
                maxlength="40"
                autocomplete="off"
                spellcheck="false"
                aria-label="Display name"
                @keyup.enter="saveName"
                @keyup.esc="editingName = false"
              />
            </div>
            <button type="button" class="btn btn--brass" :disabled="savingName" @click="saveName">
              {{ savingName ? 'Saving…' : 'Save' }}
            </button>
            <button type="button" class="btn btn--ghost" @click="editingName = false">
              Cancel
            </button>
          </div>

          <!-- Household (owner only) -->
          <div v-if="auth.isOwner" class="row">
            <div class="row__text">
              <div class="row__name">Household</div>
              <div class="row__hint">Invite or remove the people who share this library.</div>
            </div>
            <button type="button" class="btn btn--ghost" @click="router.push('/household')">
              Manage
            </button>
          </div>

          <!-- Sign out — this device only. Saying so is the whole point: people -->
          <!-- fear signing out will strand their other devices.                  -->
          <!-- Hidden when the server doesn't enforce auth: signing out of a      -->
          <!-- server that never asked you to sign in would strand you on a front -->
          <!-- door that issues no invites.                                        -->
          <div v-if="auth.enforced" class="row">
            <div class="row__text">
              <div class="row__name">Sign out</div>
              <div class="row__hint">
                This device only. Your other devices stay signed in and keep syncing.
              </div>
            </div>
            <button type="button" class="btn btn--danger" :disabled="signingOut" @click="signOut">
              {{ signingOut ? 'Signing out…' : 'Sign out' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Your devices -->
      <div v-if="auth.enforced" class="group">
        <div class="group__label">Your devices</div>
        <div class="card">
          <div v-if="devicesLoading" class="row"><div class="row__hint">Loading…</div></div>
          <div v-for="d in devices" v-else :key="d.id" class="row">
            <div class="row__text">
              <div class="row__name">
                {{ d.device_label || 'Unnamed device' }}
                <span v-if="d.current" class="badge badge--now">This device</span>
              </div>
              <div class="row__hint">{{ seenAt(d.last_seen_at) }}</div>
            </div>
            <button type="button" class="btn btn--ghost" @click="revoke(d)">
              {{ d.current ? 'Sign out' : 'Revoke' }}
            </button>
          </div>
          <div v-if="!devicesLoading && !devices.length" class="row">
            <div class="row__hint">No other devices are signed in.</div>
          </div>
        </div>
      </div>

      <!-- Connection (native shells only) -->
      <div v-if="isNative" class="group">
        <div class="group__label">Connection</div>
        <div class="card">
          <div class="row row--stack">
            <ServerSettings />
          </div>
        </div>
      </div>

      <!-- Appearance -->
      <div class="group">
        <div class="group__label">Appearance</div>
        <div class="card">
          <div class="row">
            <div class="row__text">
              <div class="row__name">Theme</div>
              <div class="row__hint">Applies to the whole app, including the reader.</div>
            </div>
            <div class="seg" role="group" aria-label="Theme">
              <button
                v-for="opt in themeOptions"
                :key="opt.value"
                type="button"
                class="seg__btn"
                :class="{ 'is-active': theme === opt.value }"
                @click="set(opt.value)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Reading -->
      <div class="group">
        <div class="group__label">Reading</div>
        <div class="card">
          <div class="row">
            <div class="row__text">
              <div class="row__name">Font</div>
              <div class="row__hint">
                Overrides every book's typeface. “Publisher” keeps the book's own.
              </div>
            </div>
            <div class="seg" role="group" aria-label="Reading font">
              <button
                v-for="opt in READING_FONTS"
                :key="opt.id"
                type="button"
                class="seg__btn"
                :class="{ 'is-active': font === opt.id }"
                :title="opt.hint"
                @click="setFont(opt.id)"
              >
                {{ opt.label }}
              </button>
            </div>
          </div>
          <div class="row">
            <p class="specimen" :style="{ fontFamily: specimenFamily }">
              The quick brown fox jumps over the lazy dog.
            </p>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.settings {
  min-height: 100%;
  background: var(--bg);
}
.settings__bar {
  display: flex;
  align-items: center;
  padding: 20px clamp(20px, 4vw, 36px);
}
.pill {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 15px;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  background: var(--glass);
  backdrop-filter: blur(8px);
  color: var(--reading);
  font: 600 13px var(--font-ui);
  cursor: pointer;
}
.pill span:first-child {
  font-size: 15px;
}

.settings__body {
  max-width: 640px;
  margin: 0 auto;
  padding: 14px clamp(20px, 4vw, 36px) 60px;
}
.settings__eyebrow {
  font: 700 12px var(--font-display);
  letter-spacing: 0.2em;
  color: var(--brass);
  text-transform: uppercase;
}
.settings__title {
  margin: 8px 0 28px;
  font: 800 clamp(28px, 4vw, 36px) var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
}

.group {
  margin-bottom: 26px;
}
.group__label {
  font: 700 11px var(--font-ui);
  letter-spacing: 0.14em;
  color: var(--dim);
  text-transform: uppercase;
  margin-bottom: 11px;
}
.card {
  border-radius: 12px;
  border: 1px solid var(--border);
  background: var(--surface);
  overflow: hidden;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  padding: 16px 18px;
}
.row + .row {
  border-top: 1px solid var(--border);
}
.row--stack {
  display: block;
}
.row__name {
  font: 600 14px var(--font-ui);
  color: var(--text);
}
.row__hint {
  font: 400 12.5px var(--font-ui);
  color: var(--muted);
  margin-top: 2px;
}

.seg {
  display: flex;
  border-radius: 999px;
  border: 1px solid var(--border-strong);
  overflow: hidden;
  flex: none;
}
.seg__btn {
  padding: 8px 16px;
  border: none;
  background: transparent;
  color: var(--dim);
  font: 700 12.5px var(--font-ui);
  cursor: pointer;
}
.seg__btn.is-active {
  background: var(--brass);
  color: var(--on-brass);
}

.specimen {
  margin: 0;
  font-size: 18px;
  line-height: 1.5;
  color: var(--reading);
}

/* Profile */
.row--identity {
  gap: 15px;
}
.acct__avatar {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  flex: none;
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
  color: var(--on-brass);
  font: 700 21px var(--font-display);
  display: flex;
  align-items: center;
  justify-content: center;
}
.acct__who {
  flex: 1;
  min-width: 0;
}
.acct__name {
  display: flex;
  align-items: center;
  gap: 9px;
  font: 700 18px var(--font-display);
  color: var(--text);
}
.acct__email {
  font: 400 13px var(--font-ui);
  color: var(--dim);
  margin-top: 3px;
}
.badge {
  padding: 2px 9px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--brass) 16%, transparent);
  border: 1px solid color-mix(in srgb, var(--brass) 40%, transparent);
  font: 800 10px var(--font-ui);
  letter-spacing: 0.06em;
  color: var(--brass-bright);
  text-transform: uppercase;
}
.badge--now {
  background: color-mix(in srgb, var(--success) 14%, transparent);
  border-color: color-mix(in srgb, var(--success) 38%, transparent);
  color: var(--success);
}
.row--editing {
  gap: 13px;
  align-items: flex-end;
  background: color-mix(in srgb, var(--brass) 4%, transparent);
}
.edit {
  flex: 1;
}
.edit__label {
  font: 600 10px var(--font-ui);
  letter-spacing: 0.1em;
  color: var(--dim);
  text-transform: uppercase;
  margin-bottom: 6px;
}
.edit__input {
  width: 100%;
  padding: 10px 13px;
  border-radius: 9px;
  background: var(--panel);
  border: 1px solid color-mix(in srgb, var(--brass) 50%, transparent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--brass) 10%, transparent);
  color: var(--text);
  font: 600 15px var(--font-ui);
  outline: none;
}
.btn {
  padding: 9px 16px;
  border-radius: 9px;
  font: 700 12.5px var(--font-ui);
  cursor: pointer;
  flex: none;
}
.btn--ghost {
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--reading);
}
.btn--brass {
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font-weight: 800;
}
.btn--brass:disabled {
  opacity: 0.6;
  cursor: progress;
}
.btn--danger {
  border: 1px solid color-mix(in srgb, var(--error) 30%, transparent);
  background: transparent;
  color: var(--error);
}
.profile__name:hover {
  border-bottom-color: var(--border-strong);
}
.profile__name::placeholder {
  color: var(--dim);
}
.profile__name:focus {
  outline: none;
  border-bottom-color: var(--brass);
}
</style>
