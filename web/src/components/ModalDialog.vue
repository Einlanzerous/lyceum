<script setup lang="ts">
// The one modal shell (LYCM-142). A native <dialog> opened with showModal(), so the
// browser supplies what the hand-rolled `div.scrim[role=dialog]` sheets never did:
// Esc, an inert page behind, a focus trap, and focus returned to whatever opened it.
//
// Callers mount it with `v-if`, like the divs it replaces; it opens on mount and
// closes on unmount. Esc and a click on the backdrop both just emit `close` — the
// parent owns the state and unmounts us. `dismissable=false` is for the one sheet
// that must be answered (signed out): Esc and the backdrop do nothing.
//
// The dialog element itself is a transparent, viewport-sized flex box, so a click
// whose target is the dialog (not something inside the sheet) is a backdrop click.
// That JS handler is the cross-browser path: `closedby="any"` is not in Firefox.
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{ dismissable?: boolean }>(), { dismissable: true })
const emit = defineEmits<{ (e: 'close'): void }>()

const el = ref<HTMLDialogElement | null>(null)

onMounted(() => {
  const d = el.value
  if (!d) return
  // jsdom has no showModal(); a plain `open` keeps component tests meaningful.
  if (typeof d.showModal === 'function') d.showModal()
  else d.setAttribute('open', '')
})

// close() (not just removing the node) is what hands focus back to the opener.
let unmounting = false
onBeforeUnmount(() => {
  unmounting = true
  const d = el.value
  if (d && typeof d.close === 'function' && d.open) d.close()
})

function onCancel(e: Event): void {
  // Esc. Never let the browser close it behind Vue's back.
  e.preventDefault()
  if (props.dismissable) emit('close')
}

// Chromium only lets `cancel` be prevented once per user activation, so a second Esc
// closes the dialog natively and skips onCancel. Whatever closed it, the parent's
// state still says "open": ask it to close, or (when it must be answered) reopen.
function onNativeClose(): void {
  if (unmounting) return
  if (props.dismissable) emit('close')
  else if (typeof el.value?.showModal === 'function') el.value.showModal()
}

function onClick(e: MouseEvent): void {
  if (props.dismissable && e.target === el.value) emit('close')
}
</script>

<template>
  <dialog ref="el" class="modal" @cancel="onCancel" @close="onNativeClose" @click="onClick">
    <slot />
  </dialog>
</template>

<style scoped>
/* Callers tune the shell with custom properties (set on the component's class). */
.modal {
  border: 0;
  padding: 0;
  margin: 0;
  max-width: none;
  max-height: none;
  width: 100%;
  height: 100%;
  background: var(--modal-bg, rgba(8, 8, 7, 0.72));
  color: inherit;
  overflow: auto;
}
.modal[open] {
  display: flex;
  align-items: var(--modal-align, center);
  justify-content: center;
  padding: var(--modal-pad, 24px);
  box-sizing: border-box;
}
.modal::backdrop {
  background: transparent;
}
@supports (backdrop-filter: blur(1px)) {
  .modal {
    backdrop-filter: blur(var(--modal-blur, 2px));
  }
}
@media (prefers-reduced-motion: no-preference) {
  .modal[open] {
    animation: modal-in 0.16s ease;
  }
}
@keyframes modal-in {
  from {
    opacity: 0;
  }
}
</style>
