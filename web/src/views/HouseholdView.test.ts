import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import HouseholdView from './HouseholdView.vue'
import { useAuthStore } from '@/stores/auth'
import * as authApi from '@/api/auth'
import type { Invite, Member, User } from '@/api/auth'

// AdminDisabledError is matched with instanceof, so the real class has to survive
// the mock.
vi.mock('@/api/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof authApi>()),
  listMembers: vi.fn(),
  requestDeviceInvite: vi.fn(),
  reinviteMember: vi.fn(),
  inviteMember: vi.fn(),
  removeMember: vi.fn(),
}))

// The QR renders through the `qrcode` package; irrelevant to what is under test.
vi.mock('@/components/InviteQr.vue', () => ({ default: { template: '<div />' } }))

const OWNER: User = { id: 1, email: 'ada@home.lan', display_name: 'Ada', is_owner: true }

const ownerRow: Member = {
  ...OWNER,
  last_seen_at: '2026-07-29T12:00:00Z',
  invite_expires_at: null,
  session_count: 1,
}
const memberRow: Member = {
  id: 7,
  email: 'mara@home.lan',
  display_name: 'Mara',
  is_owner: false,
  last_seen_at: '2026-07-28T12:00:00Z',
  invite_expires_at: null,
  session_count: 2,
}

const DEVICE_KEY: Invite = {
  user: OWNER,
  invite_token: 'lyc_owner_device_key',
  pairing_code: 'ABCD2345',
}

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/settings', component: { template: '<div />' } },
      { path: '/household', component: HouseholdView },
    ],
  })
}

async function mountView() {
  const router = makeRouter()
  await router.push('/household')
  await router.isReady()
  const wrapper = mount(HouseholdView, {
    global: {
      plugins: [router, createTestingPinia({ createSpy: vi.fn, stubActions: false })],
    },
  })
  useAuthStore().user = OWNER
  await flushPromises()
  return wrapper
}

/** The row whose name cell contains `name`. */
function rowFor(wrapper: Awaited<ReturnType<typeof mountView>>, name: string) {
  const row = wrapper.findAll('.row').find((r) => r.text().includes(name))
  if (!row) throw new Error(`no household row for ${name}`)
  return row
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(authApi.listMembers).mockResolvedValue([ownerRow, memberRow])
  vi.mocked(authApi.requestDeviceInvite).mockResolvedValue(DEVICE_KEY)
})

// The bug (LYCM-105): the owner's own row was the one row in the household with
// no action on it at all, so the person who owns the library was the only one who
// could not get a key onto a second device.
describe('your own row', () => {
  it('offers a way to add a device', async () => {
    const wrapper = await mountView()

    expect(rowFor(wrapper, 'Ada').text()).toContain('Add a device')
  })

  it('mints a key for you and shows it as yours, not as an invite to send', async () => {
    const wrapper = await mountView()

    await rowFor(wrapper, 'Ada').get('button').trigger('click')
    await flushPromises()

    expect(authApi.requestDeviceInvite).toHaveBeenCalledOnce()
    // Not the admin route: this is your own key, and members need this path too.
    expect(authApi.reinviteMember).not.toHaveBeenCalled()

    const reveal = wrapper.get('.sheet')
    expect(reveal.text()).toContain('A key for your next device')
    expect(reveal.text()).toContain('lyc_owner_device_key')
    expect(reveal.text()).not.toContain('Hand this key to')
  })

  it('still says the owner cannot be removed', async () => {
    const wrapper = await mountView()

    expect(rowFor(wrapper, 'Ada').text()).toContain("Can't be removed")
    expect(rowFor(wrapper, 'Ada').text()).not.toContain('Remove Ada')
  })
})

describe('a housemate row', () => {
  it('keeps re-invite and remove, and is not offered a device key', async () => {
    const wrapper = await mountView()
    const row = rowFor(wrapper, 'Mara')

    expect(row.text()).toContain('Re-invite')
    expect(row.text()).toContain('Remove')
    expect(row.text()).not.toContain('Add a device')
  })

  it('re-invites through the admin route and addresses the key to them', async () => {
    vi.mocked(authApi.reinviteMember).mockResolvedValue({
      user: memberRow,
      invite_token: 'lyc_mara_key',
      pairing_code: 'WXYZ6789',
    })
    const wrapper = await mountView()

    await rowFor(wrapper, 'Mara').get('button').trigger('click')
    await flushPromises()

    expect(authApi.reinviteMember).toHaveBeenCalledWith(7)
    expect(authApi.requestDeviceInvite).not.toHaveBeenCalled()
    expect(wrapper.get('.sheet').text()).toContain('Hand this key to Mara')
  })
})
