<script setup lang="ts">
// Household — owner only (LYCM-801, design frame 25).
//
// The people on this server. The owner invites, re-invites, and removes; nobody
// else can reach these routes at all (the server 403s a member, and this view is
// never routed to for one).
//
// Two states worth their own design:
//   - auth-off — accounts exist but LYCEUM_AUTH=false, so the server cannot tell
//     who is asking and refuses to mint credentials. Not a permission failure to
//     apologise for; a deliberate posture that has to be changed on the machine.
//   - the invite reveal — see InviteReveal.vue. The secret is shown once.

import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  AdminDisabledError,
  inviteMember,
  listMembers,
  reinviteMember,
  removeMember,
  type Invite,
  type Member,
} from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import InviteReveal from '@/components/InviteReveal.vue'

const router = useRouter()
const auth = useAuthStore()

const members = ref<Member[]>([])
const loading = ref(true)
const adminDisabled = ref(false)
const error = ref<string | null>(null)

const invite = ref<Invite | null>(null)
const lost = ref<{ name: string; userId: number } | null>(null)
const reissuing = ref(false)

const inviting = ref(false)
const newEmail = ref('')
const newName = ref('')
const inviteError = ref<string | null>(null)
const submitting = ref(false)

const confirmRemove = ref<Member | null>(null)
const removing = ref(false)

const canInvite = computed(() => newEmail.value.trim().length > 0 && !submitting.value)

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    members.value = await listMembers()
    adminDisabled.value = false
  } catch (err) {
    if (err instanceof AdminDisabledError) adminDisabled.value = true
    else error.value = err instanceof Error ? err.message : 'could not load the household'
  } finally {
    loading.value = false
  }
}

onMounted(load)

async function submitInvite(): Promise<void> {
  if (!canInvite.value) return
  submitting.value = true
  inviteError.value = null
  try {
    invite.value = await inviteMember(newEmail.value.trim(), newName.value.trim())
    inviting.value = false
    newEmail.value = ''
    newName.value = ''
    await load()
  } catch (err) {
    inviteError.value =
      err instanceof Error && err.message.includes('already registered')
        ? 'Someone on this server already uses that email.'
        : (err as Error).message
  } finally {
    submitting.value = false
  }
}

async function reinvite(m: Member): Promise<void> {
  try {
    invite.value = await reinviteMember(m.id)
    lost.value = null
    await load()
  } catch (err) {
    error.value = (err as Error).message
  }
}

/** Re-issue after the reveal was dismissed uncopied — the recovery path. */
async function reissue(userId: number): Promise<void> {
  reissuing.value = true
  try {
    invite.value = await reinviteMember(userId)
    lost.value = null
    await load()
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    reissuing.value = false
  }
}

/**
 * Closing the reveal is the point of no return: the plaintext exists only in this
 * component's memory, and the server kept nothing but its hash.
 *
 * If they copied it (or said they had it), just close. If they walked away from
 * it, hand over to the "that invite is gone" state — honest about what happened,
 * and offering the only real fix. Telling someone who *just copied the key* that
 * it is gone would be both wrong and alarming.
 */
function closeReveal(saved: boolean): void {
  const shown = invite.value
  invite.value = null
  lost.value = !saved && shown ? { name: shown.user.display_name, userId: shown.user.id } : null
}

async function doRemove(): Promise<void> {
  const m = confirmRemove.value
  if (!m) return
  removing.value = true
  try {
    await removeMember(m.id)
    confirmRemove.value = null
    await load()
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    removing.value = false
  }
}

function initialOf(m: { display_name: string }): string {
  return (m.display_name.trim()[0] ?? '?').toUpperCase()
}

/** "expires in 6 days" — the unit people actually reason about for an invite. */
function expiresIn(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (ms <= 0) return 'expired'
  const hours = Math.round(ms / 3_600_000)
  if (hours < 24) return `expires in ${Math.max(1, hours)} hour${hours === 1 ? '' : 's'}`
  const days = Math.round(hours / 24)
  return `expires in ${days} day${days === 1 ? '' : 's'}`
}

function lastSeen(iso: string | null): string {
  if (!iso) return 'never signed in'
  const ms = Date.now() - new Date(iso).getTime()
  const hours = Math.floor(ms / 3_600_000)
  if (hours < 24) return 'last seen today'
  const days = Math.floor(hours / 24)
  if (days === 1) return 'last seen yesterday'
  if (days < 30) return `last seen ${days} days ago`
  return 'last seen a while ago'
}

function devices(n: number): string {
  return `${n} device${n === 1 ? '' : 's'}`
}
</script>

<template>
  <section class="hh">
    <header class="hh__bar">
      <button type="button" class="pill" @click="router.push('/settings')">
        <span>‹</span><span>Settings</span>
      </button>
      <div class="hh__avatar" aria-hidden="true">{{ auth.initial }}</div>
    </header>

    <div class="hh__body">
      <div class="hh__head">
        <div>
          <div class="eyebrow">Household</div>
          <h1 class="hh__title">The people on this server</h1>
        </div>
        <button
          v-if="!adminDisabled"
          type="button"
          class="invite-btn"
          @click="inviting = !inviting"
        >
          <span aria-hidden="true">+</span> Invite someone
        </button>
      </div>

      <!-- Administration switched off. Nothing here is broken; the server is -->
      <!-- deliberately refusing to mint credentials it can't attribute. -->
      <div v-if="adminDisabled" class="locked">
        <div class="locked__icon" aria-hidden="true">🔒</div>
        <div class="locked__title">Household admin is off</div>
        <p class="locked__body">
          Accounts exist on this server, but managing them is switched off. Nothing here can be
          changed until an operator turns it on.
        </p>
        <pre class="locked__cmd">
$ export LYCEUM_AUTH=true
# then restart the server</pre>
        <p class="locked__foot">
          This has to happen on the machine running Lyceum — there's no remote switch, by design.
        </p>
      </div>

      <template v-else>
        <form v-if="inviting" class="newbie" @submit.prevent="submitInvite">
          <div class="newbie__fields">
            <label class="sr">Email</label>
            <input
              v-model="newEmail"
              class="input"
              type="email"
              placeholder="theo@home.lan"
              autocomplete="off"
            />
            <label class="sr">Name</label>
            <input v-model="newName" class="input" type="text" placeholder="Theo (optional)" />
            <button type="submit" class="btn btn--brass" :disabled="!canInvite">
              {{ submitting ? 'Creating…' : 'Create invite' }}
            </button>
            <button type="button" class="btn btn--ghost" @click="inviting = false">Cancel</button>
          </div>
          <p v-if="inviteError" class="newbie__err">{{ inviteError }}</p>
          <p v-else class="newbie__hint">
            They'll get a one-time key to paste on their device. It's shown once.
          </p>
        </form>

        <p v-if="error" class="err">{{ error }}</p>
        <p v-if="loading" class="muted">Loading the household…</p>

        <div v-else class="rows">
          <div
            v-for="m in members"
            :key="m.id"
            class="row"
            :class="{ 'row--pending': m.invite_expires_at && !m.last_seen_at }"
          >
            <div
              class="row__avatar"
              :class="{
                'row__avatar--owner': m.is_owner,
                'row__avatar--pending': m.invite_expires_at && !m.last_seen_at,
              }"
              aria-hidden="true"
            >
              {{ initialOf(m) }}
            </div>

            <div class="row__main">
              <div class="row__name">
                <span>{{ m.display_name }}</span>
                <span v-if="m.is_owner" class="badge"
                  >Owner{{ m.id === auth.user?.id ? ' · You' : '' }}</span
                >
              </div>

              <!-- Invited and never showed up. -->
              <div v-if="m.invite_expires_at && !m.last_seen_at" class="row__sub row__sub--pending">
                <i class="dot dot--pending" aria-hidden="true"></i>
                Invite pending · {{ expiresIn(m.invite_expires_at) }} · never signed in
              </div>
              <!-- The owner's own row leads with the address, not a status. -->
              <div v-else-if="m.is_owner" class="row__sub">
                {{ m.email }} · {{ devices(m.session_count) }}
              </div>
              <div v-else class="row__sub">
                <i class="dot dot--active" aria-hidden="true"></i>
                Active · {{ devices(m.session_count) }} · {{ lastSeen(m.last_seen_at) }}
              </div>
            </div>

            <span v-if="m.is_owner" class="row__note">Can't be removed</span>
            <template v-else>
              <button
                type="button"
                class="row__btn"
                :class="{ 'row__btn--pending': m.invite_expires_at && !m.last_seen_at }"
                @click="reinvite(m)"
              >
                Re-invite
              </button>
              <button type="button" class="row__btn row__btn--danger" @click="confirmRemove = m">
                Remove
              </button>
            </template>
          </div>
        </div>
      </template>
    </div>

    <InviteReveal
      :invite="invite"
      :lost="lost"
      :reissuing="reissuing"
      @close="closeReveal"
      @reissue="reissue"
    />

    <!-- Remove: the one destructive act here. Name exactly what is lost (their -->
    <!-- bookmarks) and exactly what is not (the shelf), because the fear is that -->
    <!-- removing a person removes their books. -->
    <div v-if="confirmRemove" class="scrim" role="dialog" aria-modal="true">
      <div class="confirm">
        <div class="confirm__title">Remove {{ confirmRemove.display_name }}?</div>
        <p class="confirm__body">
          This deletes {{ confirmRemove.display_name }}'s reading positions and bookmarks on every
          book. <b>The shared shelf is untouched</b> — no titles are lost. This can't be undone.
        </p>
        <div class="confirm__actions">
          <button type="button" class="btn btn--ghost" @click="confirmRemove = null">Cancel</button>
          <button type="button" class="btn btn--danger" :disabled="removing" @click="doRemove">
            {{ removing ? 'Removing…' : `Remove ${confirmRemove.display_name}` }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.hh {
  min-height: 100%;
}
.hh__bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 26px;
  border-bottom: 1px solid var(--border);
}
.pill {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 13px;
  border-radius: 999px;
  border: none;
  background: var(--hatch);
  font: 700 12.5px var(--font-ui);
  color: var(--reading);
  cursor: pointer;
}
.hh__avatar {
  margin-left: auto;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
  color: var(--on-brass);
  font: 700 12px var(--font-display);
  display: flex;
  align-items: center;
  justify-content: center;
}

.hh__body {
  max-width: 860px;
  margin: 0 auto;
  padding: 26px 30px 60px;
}
.hh__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 22px;
}
.eyebrow {
  font: 700 11px var(--font-display);
  letter-spacing: 0.2em;
  color: var(--brass);
  text-transform: uppercase;
}
.hh__title {
  font: 800 30px var(--font-display);
  color: var(--text);
  letter-spacing: -0.01em;
  margin: 5px 0 0;
}
.invite-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 20px;
  border-radius: 999px;
  border: none;
  background: var(--brass);
  color: var(--on-brass);
  font: 800 13.5px var(--font-ui);
  cursor: pointer;
  flex: none;
}

.rows {
  display: flex;
  flex-direction: column;
  gap: 9px;
}
.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 15px 18px;
  border-radius: 12px;
  background: var(--surface-raised);
  border: 1px solid var(--border);
}
.row--pending {
  background: color-mix(in srgb, var(--brass) 6%, transparent);
  border-color: color-mix(in srgb, var(--brass) 24%, transparent);
}
.row__avatar {
  width: 42px;
  height: 42px;
  border-radius: 50%;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  font: 700 16px var(--font-display);
  color: var(--on-brass);
  background: linear-gradient(135deg, #7a6a9a, #453a5c);
}
.row__avatar--owner {
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
}
/* An invite that was never redeemed isn't a person yet — the dashed ring says so
   without needing a word. */
.row__avatar--pending {
  background: color-mix(in srgb, var(--brass) 12%, transparent);
  border: 1px dashed color-mix(in srgb, var(--brass) 45%, transparent);
  color: var(--brass-bright);
}
.row__main {
  flex: 1;
  min-width: 0;
}
.row__name {
  display: flex;
  align-items: center;
  gap: 9px;
  font: 700 15px var(--font-ui);
  color: var(--text);
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
.row__sub {
  display: flex;
  align-items: center;
  gap: 7px;
  font: 400 12.5px var(--font-ui);
  color: var(--dim);
  margin-top: 2px;
}
.row__sub--pending {
  color: color-mix(in srgb, var(--brass) 60%, var(--muted));
}
.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex: none;
}
.dot--active {
  background: var(--success);
}
.dot--pending {
  background: var(--brass);
}
.row__note {
  font: 500 12px var(--font-ui);
  color: var(--dimmer);
}
.row__btn {
  padding: 8px 15px;
  border-radius: 9px;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--reading);
  font: 700 12.5px var(--font-ui);
  cursor: pointer;
  flex: none;
}
.row__btn--pending {
  border-color: color-mix(in srgb, var(--brass) 40%, transparent);
  background: color-mix(in srgb, var(--brass) 10%, transparent);
  color: var(--brass-bright);
}
.row__btn--danger {
  border-color: color-mix(in srgb, var(--error) 28%, transparent);
  color: var(--error);
}

.newbie {
  margin-bottom: 18px;
  padding: 16px 18px;
  border-radius: 12px;
  background: var(--surface-raised);
  border: 1px solid color-mix(in srgb, var(--brass) 24%, transparent);
}
.newbie__fields {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.input {
  flex: 1;
  min-width: 160px;
  padding: 10px 13px;
  border-radius: 9px;
  background: var(--panel);
  border: 1px solid var(--border-strong);
  color: var(--text);
  font: 500 13.5px var(--font-ui);
  outline: none;
}
.newbie__hint {
  margin: 10px 0 0;
  font: 400 12px var(--font-ui);
  color: var(--dim);
}
.newbie__err {
  margin: 10px 0 0;
  font: 500 12.5px var(--font-ui);
  color: var(--error);
}
.sr {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
}

.locked {
  padding: 26px;
  border-radius: 14px;
  background: var(--surface-raised);
  border: 1px solid var(--border);
  max-width: 460px;
}
.locked__icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: var(--hatch);
  border: 1px solid var(--border-strong);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
}
.locked__title {
  font: 800 18px var(--font-display);
  color: var(--text);
  margin: 16px 0 6px;
}
.locked__body {
  font: 400 13px/1.6 var(--font-ui);
  color: var(--muted);
  margin: 0;
}
.locked__cmd {
  margin: 16px 0 0;
  padding: 13px 15px;
  border-radius: 10px;
  background: var(--bg);
  border: 1px solid var(--border);
  font:
    400 12px/1.7 ui-monospace,
    monospace;
  color: var(--muted);
  white-space: pre-wrap;
}
.locked__foot {
  margin: 12px 0 0;
  font: 400 12px/1.5 var(--font-ui);
  color: var(--dim);
}

.scrim {
  position: fixed;
  inset: 0;
  z-index: 60;
  background: rgba(8, 8, 7, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.confirm {
  width: 400px;
  max-width: 100%;
  padding: 24px;
  border-radius: 14px;
  background: var(--surface-raised);
  border: 1px solid color-mix(in srgb, var(--error) 30%, transparent);
}
.confirm__title {
  font: 800 17px var(--font-display);
  color: var(--text);
  margin-bottom: 8px;
}
.confirm__body {
  font: 400 13px/1.6 var(--font-ui);
  color: color-mix(in srgb, var(--error) 40%, var(--muted));
  margin: 0;
}
.confirm__body b {
  color: var(--error);
}
.confirm__actions {
  margin-top: 18px;
  display: flex;
  gap: 10px;
}

.btn {
  padding: 11px 16px;
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
.btn--brass:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.btn--ghost {
  flex: 1;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--reading);
}
.btn--danger {
  flex: 1;
  border: none;
  background: #c0655a;
  color: #fff;
  font-weight: 800;
}
.err {
  font: 500 13px var(--font-ui);
  color: var(--error);
}
.muted {
  font: 400 13px var(--font-ui);
  color: var(--dim);
}
</style>
