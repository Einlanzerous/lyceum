import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ThemeToggle from './ThemeToggle.vue'
import { useTheme } from '@/theme'

describe('ThemeToggle', () => {
  beforeEach(() => useTheme().set('dark'))
  afterEach(() => useTheme().set('dark'))

  it('shows the moon in dark and the sun in light', async () => {
    const wrapper = mount(ThemeToggle)
    expect(wrapper.text()).toBe('☾')
    useTheme().set('light')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toBe('☀')
  })

  it('toggles the theme (and the document attribute) on click', async () => {
    const { theme } = useTheme()
    const wrapper = mount(ThemeToggle)
    expect(theme.value).toBe('dark')

    await wrapper.get('button').trigger('click')
    expect(theme.value).toBe('light')
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')

    await wrapper.get('button').trigger('click')
    expect(theme.value).toBe('dark')
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  })
})
