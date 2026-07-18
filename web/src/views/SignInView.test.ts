import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import SignInView from './SignInView.vue'
import { ApiError } from '@/api/client'
import * as authApi from '@/api/auth'

// Real auth store over a mocked API so we exercise the QR auto-redeem path
// (LYCM-88) end-to-end from the URL query.
vi.mock('@/api/auth', () => ({
  redeemInvite: vi.fn(),
  fetchMe: vi.fn(),
  signOut: vi.fn(),
  updateDisplayName: vi.fn(),
}))

// ServerSettings drags in config/native-shell concerns irrelevant here.
vi.mock('@/components/ServerSettings.vue', () => ({ default: { template: '<div />' } }))

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/sign-in', component: SignInView },
    ],
  })
}

async function mountAt(url: string) {
  const router = makeRouter()
  await router.push(url)
  await router.isReady()
  const wrapper = mount(SignInView, {
    global: { plugins: [router, createTestingPinia({ createSpy: vi.fn, stubActions: false })] },
  })
  await flushPromises()
  return { router, wrapper }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('SignInView QR auto-redeem', () => {
  it('redeems a token carried in the URL and navigates home', async () => {
    vi.mocked(authApi.redeemInvite).mockResolvedValue({
      user: { id: 1, email: 'a@b.c', display_name: 'Ada', is_owner: false },
      session_token: 'lyc_session',
    })

    const { router } = await mountAt('/sign-in?token=lyc_fromqr')

    expect(authApi.redeemInvite).toHaveBeenCalledWith('lyc_fromqr', expect.any(String))
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('extracts the token from a full scanned sign-in URL', async () => {
    vi.mocked(authApi.redeemInvite).mockResolvedValue({
      user: { id: 1, email: 'a@b.c', display_name: 'Ada', is_owner: false },
      session_token: 'lyc_session',
    })

    await mountAt('/sign-in?token=' + encodeURIComponent('http://host:8080/sign-in?token=lyc_deep'))

    expect(authApi.redeemInvite).toHaveBeenCalledWith('lyc_deep', expect.any(String))
  })

  it('scrubs the token from the URL and shows the bad-key banner on a 401', async () => {
    vi.mocked(authApi.redeemInvite).mockRejectedValue(new ApiError(401, 'nope'))

    const { router, wrapper } = await mountAt('/sign-in?token=lyc_spent')

    // Token is gone from the URL whether or not it worked...
    expect(router.currentRoute.value.query.token).toBeUndefined()
    // ...and the rejection wears the normal red banner.
    expect(wrapper.text()).toContain("That key didn't work")
  })

  it('does nothing special without a token in the URL', async () => {
    await mountAt('/sign-in')
    expect(authApi.redeemInvite).not.toHaveBeenCalled()
  })
})
