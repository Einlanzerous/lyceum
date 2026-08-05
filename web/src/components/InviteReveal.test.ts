import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import InviteReveal from './InviteReveal.vue'
import type { Invite, User } from '@/api/auth'

// The QR renders through the `qrcode` package; nothing here is about that. The
// stub still declares the props, so what this component hands the QR stays
// visible to the tests below — a prop-ignoring `<div />` would silently accept a
// reveal that passed the QR nothing at all.
vi.mock('./InviteQr.vue', () => ({
  default: { props: ['token', 'signInUrl'], template: '<div class="qr-stub" />' },
}))

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

// LYCM-102. The reveal is the only thing that hands the server's advertised
// origin to the QR; if it drops it the QR silently falls back to this browser's
// origin, which behind Cloudflare Access is the gated one a phone cannot use.
// The failure is invisible here and total on the scanning device.
describe('the sign-in URL the QR is given', () => {
  const qrProps = (wrapper: ReturnType<typeof mountReveal>) =>
    wrapper.findComponent({ name: 'InviteQr' }).props() as {
      token: string
      signInUrl?: string
    }

  it('passes the server-built URL through to the QR', () => {
    const invite = {
      ...inviteFor(MARA, 'lyc_abc'),
      sign_in_url: 'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
    }
    const wrapper = mountReveal({ invite })

    expect(qrProps(wrapper).signInUrl).toBe(
      'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
    )
    expect(qrProps(wrapper).token).toBe('lyc_abc')
  })

  it('leaves it undefined when the server sent none, so the QR falls back', () => {
    const wrapper = mountReveal({ invite: inviteFor(MARA, 'lyc_abc') })

    expect(qrProps(wrapper).signInUrl).toBeUndefined()
    expect(qrProps(wrapper).token).toBe('lyc_abc')
  })

  it('follows a re-issue to the new invite rather than keeping the spent URL', async () => {
    const wrapper = mountReveal({
      invite: {
        ...inviteFor(MARA, 'lyc_first'),
        sign_in_url: 'https://direct.example.test/sign-in?token=lyc_first',
      },
    })

    await wrapper.setProps({
      invite: {
        ...inviteFor(ME, 'lyc_second'),
        sign_in_url: 'https://direct.example.test/sign-in?token=lyc_second',
      },
    })

    expect(qrProps(wrapper).signInUrl).toBe('https://direct.example.test/sign-in?token=lyc_second')
    expect(qrProps(wrapper).token).toBe('lyc_second')
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
