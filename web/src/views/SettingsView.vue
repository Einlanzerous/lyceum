<script setup lang="ts">
// Preferences home (LYCM-501). Theme (LYCM-501) + opt-in reading font
// (LYCM-502). Both write to persisted reactive stores the reader watches, so a
// change here re-renders an open book live.
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useTheme, type Theme } from '@/theme'
import { useReadingFont } from '@/reader/readingFont'
import { READING_FONTS, resolveFontFamily } from '@/reader/font'
import { isNativeShell } from '@/api/base'
import { useProfile } from '@/profile'
import ServerSettings from '@/components/ServerSettings.vue'

const router = useRouter()
const { theme, set } = useTheme()
const { font, set: setFont } = useReadingFont()
const { name, initial, defaultName, set: setName } = useProfile()

function onName(event: Event): void {
  setName((event.target as HTMLInputElement).value)
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

      <!-- Profile -->
      <div class="group">
        <div class="group__label">Profile</div>
        <div class="card">
          <div class="row">
            <div class="profile">
              <div class="profile__avatar" aria-hidden="true">{{ initial }}</div>
              <input
                class="profile__name"
                type="text"
                :value="name"
                :placeholder="defaultName"
                maxlength="40"
                autocomplete="off"
                spellcheck="false"
                aria-label="Display name"
                @input="onName"
              />
            </div>
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
.profile {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
}
.profile__avatar {
  width: 46px;
  height: 46px;
  flex: none;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--brass), #7a5d2c);
  color: var(--on-brass);
  display: flex;
  align-items: center;
  justify-content: center;
  font: 700 18px var(--font-display);
}
.profile__name {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  border-bottom: 1px solid transparent;
  color: var(--text);
  font: 800 20px var(--font-display);
  padding: 4px 0;
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
