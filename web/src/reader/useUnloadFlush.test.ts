import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { useUnloadFlush } from './useUnloadFlush'

function host(flush: () => void) {
  return mount(
    defineComponent({
      setup() {
        useUnloadFlush(flush)
        return () => h('div')
      },
    }),
  )
}

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true })
}

afterEach(() => {
  setVisibility('visible')
})

describe('useUnloadFlush', () => {
  it('flushes on pagehide', () => {
    const flush = vi.fn()
    const wrapper = host(flush)
    window.dispatchEvent(new Event('pagehide'))
    expect(flush).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('flushes when the document becomes hidden, not when it becomes visible', () => {
    const flush = vi.fn()
    const wrapper = host(flush)

    setVisibility('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(flush).not.toHaveBeenCalled()

    setVisibility('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(flush).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('detaches its listeners on unmount', () => {
    const flush = vi.fn()
    const wrapper = host(flush)
    wrapper.unmount()
    window.dispatchEvent(new Event('pagehide'))
    setVisibility('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(flush).not.toHaveBeenCalled()
  })
})
