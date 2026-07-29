import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useInviteReveal } from './useInviteReveal'
import { useAuthStore } from '@/stores/auth'
import * as authApi from '@/api/auth'
import type { Invite, User } from '@/api/auth'

vi.mock('@/api/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof authApi>()),
  requestDeviceInvite: vi.fn(),
  reinviteMember: vi.fn(),
}))

const ME: User = { id: 1, email: 'ada@home.lan', display_name: 'Ada', is_owner: true }
const MARA: User = { id: 7, email: 'mara@home.lan', display_name: 'Mara', is_owner: false }

const inviteFor = (user: User): Invite => ({
  user,
  invite_token: `lyc_${user.id}_key`,
  pairing_code: 'ABCD2345',
})

beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  useAuthStore().user = ME
  vi.mocked(authApi.requestDeviceInvite).mockResolvedValue(inviteFor(ME))
  vi.mocked(authApi.reinviteMember).mockResolvedValue(inviteFor(MARA))
})

describe('addDevice', () => {
  it('mints a key for the signed-in person and marks the reveal as their own', async () => {
    const r = useInviteReveal()

    await r.addDevice()

    expect(authApi.requestDeviceInvite).toHaveBeenCalledOnce()
    expect(r.invite.value?.invite_token).toBe('lyc_1_key')
    expect(r.self.value).toBe(true)
  })

  it('reports a refusal instead of opening an empty reveal', async () => {
    vi.mocked(authApi.requestDeviceInvite).mockRejectedValue(new authApi.AdminDisabledError())
    const r = useInviteReveal()

    await r.addDevice()

    expect(r.invite.value).toBeNull()
    expect(r.error.value).toMatch(/switched off/)
  })

  // A home server on a slow LAN leaves a real window to click twice, and each
  // mint invalidates the one before it — so the second click would hand back a key
  // the first reveal is already showing as valid.
  it('ignores a second click while the first is still in flight', async () => {
    let release = (): void => {}
    vi.mocked(authApi.requestDeviceInvite).mockImplementation(
      () => new Promise((resolve) => (release = () => resolve(inviteFor(ME)))),
    )
    const r = useInviteReveal()

    const first = r.addDevice()
    await r.addDevice()
    release()
    await first

    expect(authApi.requestDeviceInvite).toHaveBeenCalledOnce()
  })
})

describe('reinvite', () => {
  it("does not claim a housemate's key as your own", async () => {
    const r = useInviteReveal()

    await r.reinvite(MARA.id)

    expect(authApi.reinviteMember).toHaveBeenCalledWith(MARA.id)
    expect(r.self.value).toBe(false)
  })
})

describe('close', () => {
  it('keeps quiet when the key was saved', async () => {
    const r = useInviteReveal()
    await r.addDevice()

    r.close(true)

    expect(r.invite.value).toBeNull()
    expect(r.lost.value).toBeNull()
  })

  it('hands a dismissed key to the recovery path, remembering whose it was', async () => {
    const r = useInviteReveal()
    await r.addDevice()

    r.close(false)

    expect(r.invite.value).toBeNull()
    expect(r.lost.value).toEqual({ name: 'Ada', userId: 1, self: true })
  })

  it('remembers a dismissed housemate key as not your own', async () => {
    const r = useInviteReveal()
    await r.reinvite(MARA.id)

    r.close(false)

    expect(r.lost.value).toEqual({ name: 'Mara', userId: 7, self: false })
  })
})

describe('reissue', () => {
  // The recovery path has to go back down the road it came from. Re-issuing your
  // own device key through the admin route would 403 for every non-owner, and
  // re-issuing a housemate's through the self route would silently mint a key for
  // the wrong person.
  it('re-mints your own key through the self route', async () => {
    const r = useInviteReveal()
    await r.addDevice()
    r.close(false)
    vi.mocked(authApi.requestDeviceInvite).mockClear()

    await r.reissue()

    expect(authApi.requestDeviceInvite).toHaveBeenCalledOnce()
    expect(authApi.reinviteMember).not.toHaveBeenCalled()
    expect(r.lost.value).toBeNull()
    expect(r.invite.value?.user.id).toBe(ME.id)
  })

  it("re-mints a housemate's key through the admin route", async () => {
    const r = useInviteReveal()
    await r.reinvite(MARA.id)
    r.close(false)

    await r.reissue()

    expect(authApi.requestDeviceInvite).not.toHaveBeenCalled()
    expect(authApi.reinviteMember).toHaveBeenLastCalledWith(MARA.id)
    expect(r.invite.value?.user.id).toBe(MARA.id)
  })

  it('does nothing when no key was lost', async () => {
    const r = useInviteReveal()

    await r.reissue()

    expect(authApi.requestDeviceInvite).not.toHaveBeenCalled()
    expect(authApi.reinviteMember).not.toHaveBeenCalled()
  })
})

describe('onMinted', () => {
  it('fires after each mint so a caller can re-read what changed', async () => {
    const onMinted = vi.fn()
    const r = useInviteReveal(onMinted)

    await r.addDevice()
    await r.reinvite(MARA.id)

    expect(onMinted).toHaveBeenCalledTimes(2)
  })
})
