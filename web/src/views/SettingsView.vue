<script setup lang="ts">
// Preferences home (LYCM-501). Today: theme. The Reading section reserves the
// slot for the opt-in reading font (LYCM-502).
import { useRouter } from 'vue-router'
import { useTheme, type Theme } from '@/theme'

const router = useRouter()
const { theme, set } = useTheme()

const themeOptions: { value: Theme; label: string }[] = [
  { value: 'dark', label: 'Dark' },
  { value: 'light', label: 'Light' },
]
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
              <div class="row__hint">Books render in their publisher's typography.</div>
            </div>
            <span class="badge">Publisher · default</span>
          </div>
          <div class="row row--muted">
            <div class="row__text">
              <div class="row__name">Custom reading font</div>
              <div class="row__hint">Pick your own typeface for every book.</div>
            </div>
            <span class="badge badge--soon">Coming soon</span>
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
.row--muted {
  opacity: 0.66;
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

.badge {
  flex: none;
  padding: 5px 11px;
  border-radius: 999px;
  border: 1px solid rgba(201, 154, 78, 0.3);
  background: rgba(201, 154, 78, 0.1);
  color: var(--brass-bright);
  font: 700 11px var(--font-ui);
}
.badge--soon {
  border-color: var(--border-strong);
  background: transparent;
  color: var(--dim);
}
</style>
