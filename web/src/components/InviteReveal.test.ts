import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import InviteReveal from './InviteReveal.vue'
import type { Invite, User } from '@/api/auth'

// The QR renders through the `qrcode` package; nothing here is about that.
vi.mock('./InviteQr.vue', () => ({ default: { template: '<div />' } }))

const ME: User = { id: 1, email: 'ada@home.lan', display_name: 'Ada', is_owner: true }
const MARA: User = { id: 7, email: 'mara@home.lan', display_name: 'Mara', is_owner: false }

const inviteFor = (user: User, token: string): Invite => ({
  user,
  invite_token: token,
  pairing_code: 'ABCD2345',
})

function mountReveal(props: Partial<InstanceType<typeof InviteReveal>['$props']> = {}) {
  return mount(InviteReveal, {
    props: { invite: null, lost: null, ...props } as never,
  })
}

beforeEach(() => {
  vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
})

// The whole sheet turns on one question: has this key got out? Answer it from
// stale state and someone loses a credential believing they copied it.
describe('a second reveal in the same session', () => {
  // Both parents render this component unconditionally, so the instance — and its
  // copied/copyFailed/codeCopied refs — outlive any one reveal. Before LYCM-105 a
  // second mint was rare; "Add a device" makes it routine.
  it('does not inherit the previous key\'s "copied" state', async () => {
    const wrapper = mountReveal({ invite: inviteFor(MARA, 'lyc_first') })

    await wrapper.get('.secret__copy').trigger('click')
    await flushPromises()
    expect(wrapper.get('.secret__copy').text()).toContain('Copied')

    // A fresh key arrives (a re-issue, or the next "Add a device").
    await wrapper.setProps({ invite: inviteFor(ME, 'lyc_second') })

    expect(wrapper.get('.secret__copy').text()).toContain('Copy key')
    expect(wrapper.text()).toContain('lyc_second')
  })

  it('shows the once-only warning again, instead of a false success', async () => {
    const wrapper = mountReveal({ invite: inviteFor(MARA, 'lyc_first') })

    await wrapper.get('.secret__copy').trigger('click')
    await flushPromises()
    // Copied: the warning is replaced by the reassuring note.
    expect(wrapper.text()).not.toContain("This is the only time you'll see this key.")

    await wrapper.setProps({ invite: inviteFor(ME, 'lyc_second') })

    // The new key has *not* been copied, and the sheet must say so — this is the
    // warning that stops someone closing on an unrecoverable secret.
    expect(wrapper.text()).toContain("This is the only time you'll see this key.")
    expect(wrapper.text()).not.toContain('Copied to clipboard')
  })

  it('clears a stuck clipboard failure', async () => {
    vi.stubGlobal('navigator', {
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error('insecure origin')) },
    })
    const wrapper = mountReveal({ invite: inviteFor(MARA, 'lyc_first') })

    await wrapper.get('.secret__copy').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain("Couldn't reach the clipboard")

    await wrapper.setProps({ invite: inviteFor(ME, 'lyc_second') })

    expect(wrapper.text()).not.toContain("Couldn't reach the clipboard")
  })
})

describe('the recovery sheet', () => {
  // The page behind is under an opaque scrim, so an error rendered out there is
  // an error nobody sees — and a dead "issue another" button looks identical to
  // one that simply did nothing.
  it('shows a failed re-issue inside the modal', () => {
    const wrapper = mountReveal({
      lost: { name: 'Ada', userId: 1, self: true },
      error: 'the server refused',
    })

    expect(wrapper.text()).toContain('the server refused')
  })

  it('emits reissue without a payload — the parent knows whose key it was', async () => {
    const wrapper = mountReveal({ lost: { name: 'Ada', userId: 1, self: true } })

    await wrapper.get('.btn--brass').trigger('click')

    expect(wrapper.emitted('reissue')).toHaveLength(1)
    expect(wrapper.emitted('reissue')![0]).toEqual([])
  })
})
