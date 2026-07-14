<script setup lang="ts">
// The library header avatar, now that it has a real identity behind it (LYCM-801).
//
// It used to be a bare link to Settings whose letter came from a name in this
// browser. It is now the account: who you are, where to manage the household (if
// it's yours), and the way out.
//
// "Reading as" is shown deliberately. Lyceum supports two people reading the same
// book on the same living-room tablet, so on a shared device the single most
// useful thing this menu can do is answer "whose progress am I about to move?"
// before you open a book, not after.

import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

function onDocClick(e: MouseEvent): void {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
function onEsc(e: KeyboardEvent): void {
  if (e.key === 'Escape') open.value = false
}
onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onEsc)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onEsc)
})

async function go(path: string): Promise<void> {
  open.value = false
  await router.push(path)
}

async function signOut(): Promise<void> {
  open.value = false
  await auth.signOut()
  await router.push('/sign-in')
}
</script>

<template>
  <div ref="root" class="am">
    <button
      type="button"
      class="am__avatar"
      :aria-expanded="open"
      aria-haspopup="menu"
      :title="auth.displayName || 'Account'"
      @click="open = !open"
    >
      {{ auth.initial }}
    </button>

    <div v-if="open" class="am__menu" role="menu">
      <div class="am__who">
        <div class="am__who-avatar" aria-hidden="true">{{ auth.initial }}</div>
        <div class="am__who-text">
          <div class="am__eyebrow">Reading as</div>
          <div class="am__name">
            {{ auth.displayName }}
            <span v-if="auth.isOwner" class="am__badge">Owner</span>
          </div>
          <div class="am__email">{{ auth.user?.email }}</div>
        </div>
      </div>

      <button type="button" class="am__item" role="menuitem" @click="go('/settings')">
        Settings
      </button>
      <button
        v-if="auth.isOwner"
        type="button"
        class="am__item"
        role="menuitem"
        @click="go('/household')"
      >
        Household
      </button>
      <button
        v-if="auth.enforced"
        type="button"
        class="am__item am__item--out"
        role="menuitem"
        @click="signOut"
      >
        Sign out
        <span class="am__hint">this device only</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.am {
  position: relative;
}
.am__avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: none;
  cursor: pointer;
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
  color: var(--on-brass);
  font: 700 13px var(--font-display);
  display: flex;
  align-items: center;
  justify-content: center;
}
.am__menu {
  position: absolute;
  right: 0;
  top: calc(100% + 8px);
  z-index: 40;
  width: 250px;
  padding: 6px;
  border-radius: 14px;
  background: var(--surface-raised);
  border: 1px solid var(--border-strong);
  box-shadow: var(--shadow-pop);
}
.am__who {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 12px 12px 13px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 6px;
}
.am__who-avatar {
  width: 38px;
  height: 38px;
  border-radius: 50%;
  flex: none;
  background: linear-gradient(135deg, var(--brass-bright), var(--brass));
  color: var(--on-brass);
  font: 700 15px var(--font-display);
  display: flex;
  align-items: center;
  justify-content: center;
}
.am__who-text {
  min-width: 0;
}
.am__eyebrow {
  font: 700 9px var(--font-ui);
  letter-spacing: 0.14em;
  color: var(--brass);
  text-transform: uppercase;
}
.am__name {
  display: flex;
  align-items: center;
  gap: 7px;
  font: 700 14.5px var(--font-ui);
  color: var(--text);
  margin-top: 2px;
}
.am__badge {
  padding: 1px 7px;
  border-radius: 5px;
  background: color-mix(in srgb, var(--brass) 16%, transparent);
  border: 1px solid color-mix(in srgb, var(--brass) 38%, transparent);
  font: 800 9px var(--font-ui);
  letter-spacing: 0.06em;
  color: var(--brass-bright);
  text-transform: uppercase;
}
.am__email {
  font: 400 11.5px var(--font-ui);
  color: var(--dim);
  margin-top: 1px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.am__item {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 10px 12px;
  border: none;
  border-radius: 9px;
  background: transparent;
  color: var(--reading);
  font: 600 13.5px var(--font-ui);
  text-align: left;
  cursor: pointer;
}
.am__item:hover {
  background: var(--hatch);
}
.am__item--out {
  color: var(--error);
}
.am__hint {
  font: 400 11px var(--font-ui);
  color: var(--dim);
}
</style>
