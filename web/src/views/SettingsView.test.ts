import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import SettingsView from './SettingsView.vue'
import { useAuthStore } from '@/stores/auth'
import { __resetSessionCache, setSessionToken } from '@/api/http'
import * as authApi from '@/api/auth'
import type { Invite, User } from '@/api/auth'

vi.mock('@/api/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof authApi>()),
  listDevices: vi.fn(),
  revokeDevice: vi.fn(),
  requestDeviceInvite: vi.fn(),
}))

// Neither is what's under test: one talks to a native shell's config, the other
// renders a QR through the `qrcode` package.
vi.mock('@/components/ServerSettings.vue', () => ({ default: { template: '<div />' } }))
vi.mock('@/components/InviteQr.vue', () => ({ default: { template: '<div />' } }))

const MARA: User = { id: 7, email: 'mara@home.lan', display_name: 'Mara', is_owner: false }

const DEVICE_KEY: Invite = {
  user: MARA,
  invite_token: 'lyc_mara_device_key',
  pairing_code: 'ABCD2345',
}

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/sign-in', component: { template: '<div />' } },
      { path: '/household', component: { template: '<div />' } },
      { path: '/settings', component: SettingsView },
    ],
  })
}

async function mountView(user: User = MARA) {
  const router = makeRouter()
  await router.push('/settings')
  await router.isReady()
  const wrapper = mount(SettingsView, {
    global: {
      plugins: [router, createTestingPinia({ createSpy: vi.fn, stubActions: false })],
    },
  })
  useAuthStore().user = user
  await flushPromises()
  return wrapper
}

/** The "Your devices" card, which is where a person goes looking for this. */
function devicesGroup(wrapper: Awaited<ReturnType<typeof mountView>>) {
  const group = wrapper.findAll('.group').find((g) => g.text().includes('Your devices'))
  if (!group) throw new Error('no "Your devices" group rendered')
  return group
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  __resetSessionCache()
  // "Your devices" only exists on a server that enforces auth — see auth.enforced.
  setSessionToken('lyc_session')
  vi.mocked(authApi.listDevices).mockResolvedValue([])
  vi.mocked(authApi.requestDeviceInvite).mockResolvedValue(DEVICE_KEY)
})

// This is where the reporter of LYCM-105 went looking, found a list of devices
// they could only take away, and gave up.
describe('Your devices', () => {
  it('offers a way to add one', async () => {
    const wrapper = await mountView()

    expect(devicesGroup(wrapper).text()).toContain('Add a device')
  })

  // Not owner-gated: a housemate pairing their own phone needs no authority over
  // the household, which is why this does not go through /admin.
  it('offers it to a housemate, not just the owner', async () => {
    const wrapper = await mountView({ ...MARA, is_owner: false })

    const button = devicesGroup(wrapper)
      .findAll('button')
      .find((b) => b.text().includes('Get a key'))
    expect(button).toBeDefined()
  })

  it("reveals the minted key as the viewer's own", async () => {
    const wrapper = await mountView()

    const button = devicesGroup(wrapper)
      .findAll('button')
      .find((b) => b.text().includes('Get a key'))!
    await button.trigger('click')
    await flushPromises()

    expect(authApi.requestDeviceInvite).toHaveBeenCalledOnce()
    const reveal = wrapper.get('.sheet')
    expect(reveal.text()).toContain('A key for your next device')
    expect(reveal.text()).toContain('lyc_mara_device_key')
  })

  it('explains a server that refuses to mint instead of failing silently', async () => {
    vi.mocked(authApi.requestDeviceInvite).mockRejectedValue(new authApi.AdminDisabledError())
    const wrapper = await mountView()

    const button = devicesGroup(wrapper)
      .findAll('button')
      .find((b) => b.text().includes('Get a key'))!
    await button.trigger('click')
    await flushPromises()

    expect(wrapper.find('.sheet').exists()).toBe(false)
    expect(devicesGroup(wrapper).text()).toContain('switched off')
  })
})
