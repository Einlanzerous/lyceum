import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ModalDialog from './ModalDialog.vue'

describe('ModalDialog (LYCM-142)', () => {
  it('renders a native <dialog> that is open', () => {
    const w = mount(ModalDialog, { slots: { default: '<p>hi</p>' }, attachTo: document.body })
    const d = w.find('dialog').element as HTMLDialogElement
    expect(d.hasAttribute('open')).toBe(true)
    w.unmount()
  })

  it('cancels Esc itself and asks the parent to close', async () => {
    const w = mount(ModalDialog, { attachTo: document.body })
    const ev = new Event('cancel', { cancelable: true })
    w.find('dialog').element.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('closes on a backdrop click but not a click inside the sheet', async () => {
    const w = mount(ModalDialog, {
      slots: { default: '<div class="sheet">x</div>' },
      attachTo: document.body,
    })
    await w.find('.sheet').trigger('click')
    expect(w.emitted('close')).toBeUndefined()
    await w.find('dialog').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('does nothing on Esc or the backdrop when it is not dismissable', async () => {
    const w = mount(ModalDialog, { props: { dismissable: false }, attachTo: document.body })
    const ev = new Event('cancel', { cancelable: true })
    w.find('dialog').element.dispatchEvent(ev)
    await w.find('dialog').trigger('click')
    expect(ev.defaultPrevented).toBe(true)
    expect(w.emitted('close')).toBeUndefined()
    w.unmount()
  })

  it('follows a native close (second Esc in Chromium) when dismissable', async () => {
    const w = mount(ModalDialog, { attachTo: document.body })
    await w.find('dialog').trigger('close')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('reopens after a native close when it must be answered', async () => {
    const w = mount(ModalDialog, { props: { dismissable: false }, attachTo: document.body })
    const d = w.find('dialog').element as HTMLDialogElement
    let reopened = 0
    d.showModal = () => {
      reopened++
    }
    await w.find('dialog').trigger('close')
    expect(reopened).toBe(1)
    expect(w.emitted('close')).toBeUndefined()
    w.unmount()
  })
})
