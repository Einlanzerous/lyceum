<script setup lang="ts">
// Native-shell update nudge (LYCM-300): a slim, dismissible banner shown when a
// newer GitHub release is available. Hidden entirely in the web build and until
// checkForUpdate() (called from App on mount) finds one. See update/useUpdate.ts.
import { useUpdate } from '@/update/useUpdate'

const { update, dismiss, openDownload } = useUpdate()
</script>

<template>
  <div v-if="update" class="upd" role="status">
    <span class="upd__msg">
      Lyceum <strong>{{ update.version }}</strong> is available.
    </span>
    <button type="button" class="upd__btn" @click="openDownload">Download</button>
    <button type="button" class="upd__x" aria-label="Dismiss update notice" @click="dismiss">
      &times;
    </button>
  </div>
</template>

<style scoped>
.upd {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 1rem;
  background: var(--surface-raised, #201f1c);
  color: var(--text, #eaeae5);
  border-bottom: 1px solid var(--brass, #c99a4e);
  font-size: 0.9rem;
}

.upd__msg {
  flex: 1;
  min-width: 0;
}

.upd__btn {
  padding: 0.3rem 0.85rem;
  border: 0;
  border-radius: 6px;
  background: var(--brass, #c99a4e);
  color: var(--on-brass, #171717);
  font-weight: 600;
  cursor: pointer;
}

.upd__btn:hover {
  background: var(--brass-bright, #ddb066);
}

.upd__x {
  padding: 0 0.4rem;
  border: 0;
  background: transparent;
  color: var(--muted, #9a9a92);
  font-size: 1.2rem;
  line-height: 1;
  cursor: pointer;
}

.upd__x:hover {
  color: var(--text, #eaeae5);
}
</style>
