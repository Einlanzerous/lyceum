import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ServerSettings from './ServerSettings.vue'

describe('ServerSettings Enter handling (LYCM-142)', () => {
  beforeEach(() => localStorage.clear())

  it('saves on a real Enter but not on the Enter that confirms an IME composition', async () => {
    const w = mount(ServerSettings)
    const input = w.find('input')
    await input.setValue('http://192.168.1.10:8080')

    await input.trigger('keydown', { key: 'Enter', isComposing: true })
    expect(w.emitted('saved')).toBeUndefined()

    await input.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('saved')).toHaveLength(1)
  })
})
