<script setup lang="ts">
// Backend connection editor for the native shells (LYCM-300). Shown in Settings
// and as the Library's first-run prompt. Lets the user point the app at their
// Lyceum server, test reachability (GET /healthz), and save. In the web build
// the app is served by the backend same-origin, so this is never rendered.
import { ref } from 'vue'
import { useServer } from '@/api/useServer'
import { normalizeServerUrl } from '@/api/base'

const emit = defineEmits<{ saved: [] }>()

const { server, save } = useServer()
const draft = ref(server.value)

type Status = { kind: 'ok' | 'error' | 'testing'; message: string }
const status = ref<Status | null>(null)

function onSave(): void {
  const url = normalizeServerUrl(draft.value)
  if (!url) {
    status.value = { kind: 'error', message: 'Enter your server URL.' }
    return
  }
  save(url)
  draft.value = url
  status.value = { kind: 'ok', message: 'Saved.' }
  emit('saved')
}

async function onTest(): Promise<void> {
  const url = normalizeServerUrl(draft.value)
  if (!url) {
    status.value = { kind: 'error', message: 'Enter your server URL.' }
    return
  }
  status.value = { kind: 'testing', message: 'Contacting server…' }
  try {
    const res = await fetch(`${url}/healthz`)
    if (!res.ok) {
      status.value = { kind: 'error', message: `Server responded ${res.status}.` }
      return
    }
    status.value = { kind: 'ok', message: 'Reached the server.' }
  } catch {
    // A network/CORS failure lands here — the most common first-run mistake.
    status.value = {
      kind: 'error',
      message: 'Could not reach the server. Check the URL and that it is running.',
    }
  }
}
</script>

<template>
  <div class="conn">
    <label class="conn__label" for="server-url">Server URL</label>
    <p class="conn__hint">
      The address of your Lyceum server, e.g. <code>http://192.168.1.10:8080</code>.
    </p>
    <div class="conn__row">
      <input
        id="server-url"
        v-model="draft"
        class="conn__input"
        type="url"
        inputmode="url"
        autocapitalize="off"
        autocorrect="off"
        spellcheck="false"
        placeholder="http://your-server:8080"
        @keyup.enter="onSave"
      />
      <button type="button" class="conn__btn conn__btn--ghost" @click="onTest">Test</button>
      <button type="button" class="conn__btn conn__btn--brass" @click="onSave">Save</button>
    </div>
    <p v-if="status" class="conn__status" :class="`conn__status--${status.kind}`" role="status">
      {{ status.message }}
    </p>
  </div>
</template>

<style scoped>
.conn {
  display: flex;
  flex-direction: column;
}
.conn__label {
  font: 600 14px var(--font-ui);
  color: var(--text);
}
.conn__hint {
  font: 400 12.5px var(--font-ui);
  color: var(--muted);
  margin: 4px 0 12px;
}
.conn__hint code {
  font-family: var(--font-mono, monospace);
  color: var(--reading);
}
.conn__row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.conn__input {
  flex: 1 1 220px;
  min-width: 0;
  padding: 10px 13px;
  border-radius: 9px;
  border: 1px solid var(--border-strong);
  background: var(--bg);
  color: var(--reading);
  font: 500 14px var(--font-ui);
}
.conn__input:focus {
  outline: none;
  border-color: var(--brass);
}
.conn__btn {
  padding: 10px 16px;
  border-radius: 9px;
  font: 700 13px var(--font-ui);
  cursor: pointer;
  flex: none;
}
.conn__btn--brass {
  border: none;
  background: var(--brass);
  color: var(--on-brass);
}
.conn__btn--ghost {
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--text);
}
.conn__status {
  margin: 11px 0 0;
  font: 600 12.5px var(--font-ui);
}
.conn__status--ok {
  color: var(--success);
}
.conn__status--error {
  color: var(--error);
}
.conn__status--testing {
  color: var(--muted);
}
</style>
