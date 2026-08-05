import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import InviteQr from './InviteQr.vue'
import { __resetServerCache, __setNativeShell, setServerUrl } from '@/api/base'

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

afterEach(() => {
  __setNativeShell(null)
  setServerUrl('')
  __resetServerCache()
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

  // The Wails desktop shell serves this SPA from wails.localhost while the
  // backend is a remote server. Encoding the shell's own origin would produce a
  // QR that resolves on no device but this one — and unlike the browser build,
  // there is no same-origin backend for it to accidentally be right about.
  it('uses the configured backend, not the shell origin, in a native build', async () => {
    __setNativeShell(true)
    setServerUrl('https://lyceum-direct.example.test')

    mount(InviteQr, { props: { token: 'lyc_abc' } })
    await flushPromises()

    expect(encoded()).toBe('https://lyceum-direct.example.test/sign-in?token=lyc_abc')
    expect(encoded()).not.toContain(window.location.origin)
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

// Re-issue makes two renders in flight an ordinary sequence: mint a key, dismiss
// without copying, mint another. If the first render resolves last it would paint
// the spent invite's QR beside the live invite's key — a code that scans
// perfectly and signs nobody in, with nothing on screen to say which is which.
describe('when two renders overlap', () => {
  it('keeps the newest QR even if an earlier render resolves last', async () => {
    const resolvers: Array<(v: string) => void> = []
    toDataURL
      .mockReset()
      .mockImplementation(() => new Promise<string>((resolve) => resolvers.push(resolve)))

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

    expect(resolvers).toHaveLength(2)

    // The re-issue's QR lands first, then the stale one for the spent invite.
    resolvers[1]('data:image/png;base64,second')
    resolvers[0]('data:image/png;base64,first')
    await flushPromises()

    expect(wrapper.get('.qr__img').attributes('src')).toBe('data:image/png;base64,second')
  })

  it('does not let a stale failure blank the current QR', async () => {
    const first: Array<(reason: Error) => void> = []
    let secondResolve: ((v: string) => void) | undefined
    toDataURL
      .mockReset()
      .mockImplementationOnce(() => new Promise<string>((_, reject) => first.push(reject)))
      .mockImplementationOnce(() => new Promise<string>((resolve) => (secondResolve = resolve)))

    const wrapper = mount(InviteQr, { props: { token: 'lyc_first' } })
    await flushPromises()
    await wrapper.setProps({ token: 'lyc_second' })
    await flushPromises()

    secondResolve?.('data:image/png;base64,second')
    await flushPromises()
    // The abandoned render now fails; it must not take the live QR down with it.
    first[0]?.(new Error('too long to encode'))
    await flushPromises()

    expect(wrapper.find('.qr').exists()).toBe(true)
    expect(wrapper.get('.qr__img').attributes('src')).toBe('data:image/png;base64,second')
  })
})
