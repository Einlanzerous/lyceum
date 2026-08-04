import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import InviteQr from './InviteQr.vue'

// Capture what gets encoded; the `qrcode` package's own rendering is not the
// subject here, the choice of URL is.
const toDataURL = vi.fn()
vi.mock('qrcode', () => ({
  default: { toDataURL: (...args: unknown[]) => toDataURL(...args) },
}))

/** The URL the QR was actually built from. */
const encoded = () => toDataURL.mock.calls.at(-1)?.[0] as string

beforeEach(() => {
  toDataURL.mockReset().mockResolvedValue('data:image/png;base64,stub')
})

// LYCM-102. The origin this browser is on is not necessarily one a phone can
// reach: behind Cloudflare Access the owner mints invites on the SSO-gated host,
// and a QR built from `window.location` walks the scanner into a login wall that
// a bearer token cannot open. The server knows which origin to advertise, so when
// it says, it wins.
describe('which origin the QR encodes', () => {
  it('uses the server-provided sign-in URL over this browser origin', async () => {
    mount(InviteQr, {
      props: {
        token: 'lyc_abc',
        signInUrl: 'https://lyceum-direct.example.test/sign-in?token=lyc_abc',
      },
    })
    await flushPromises()

    expect(encoded()).toBe('https://lyceum-direct.example.test/sign-in?token=lyc_abc')
    expect(encoded()).not.toContain(window.location.origin)
  })

  // LAN and dev: no mobile base URL configured, so the server sends nothing and
  // the origin in the address bar is genuinely the one to hand out.
  it('falls back to this origin when the server sends none', async () => {
    mount(InviteQr, { props: { token: 'lyc_abc' } })
    await flushPromises()

    expect(encoded()).toBe(`${window.location.origin}/sign-in?token=lyc_abc`)
  })

  // An empty string is what a misconfigured or half-populated payload looks like;
  // encoding it would produce a QR that resolves nowhere.
  it('treats a blank sign-in URL as absent rather than encoding it', async () => {
    mount(InviteQr, { props: { token: 'lyc_abc', signInUrl: '   ' } })
    await flushPromises()

    expect(encoded()).toBe(`${window.location.origin}/sign-in?token=lyc_abc`)
  })

  it('re-encodes when a re-issued invite arrives', async () => {
    const wrapper = mount(InviteQr, {
      props: {
        token: 'lyc_first',
        signInUrl: 'https://direct.example.test/sign-in?token=lyc_first',
      },
    })
    await flushPromises()

    await wrapper.setProps({
      token: 'lyc_second',
      signInUrl: 'https://direct.example.test/sign-in?token=lyc_second',
    })
    await flushPromises()

    expect(encoded()).toBe('https://direct.example.test/sign-in?token=lyc_second')
  })
})
