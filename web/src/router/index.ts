import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/sign-in',
    name: 'sign-in',
    component: () => import('@/views/SignInView.vue'),
    // The front door owns its whole surface, and it is the one route reachable
    // without a session.
    meta: { chromeless: true, public: true },
  },
  {
    path: '/household',
    name: 'household',
    component: () => import('@/views/HouseholdView.vue'),
    meta: { owner: true },
  },
  {
    path: '/',
    name: 'library',
    component: () => import('@/views/LibraryView.vue'),
  },
  {
    path: '/reader/:id',
    name: 'reader',
    component: () => import('@/views/ReaderView.vue'),
    props: true,
    meta: { chromeless: true },
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('@/views/SettingsView.vue'),
  },
  {
    path: '/ingest',
    name: 'ingest',
    component: () => import('@/views/IngestVerifyView.vue'),
  },
  {
    path: '/review',
    name: 'review',
    component: () => import('@/views/ReviewView.vue'),
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

/**
 * Gate every route on knowing who we are (LYCM-801).
 *
 * `auth.load()` runs once, lazily, on the first navigation — not at module import
 * — so the app never renders a shelf before it knows whose shelf it is. On a
 * server with enforcement off, /auth/me answers with the owner and this is
 * invisible: nobody is ever sent to the front door.
 */
router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (auth.status === 'unknown') {
    try {
      await auth.load()
    } catch {
      // The server is unreachable. Let the route render and fail in its own way
      // (the library shows a connect prompt); bouncing to a sign-in screen that
      // also can't reach the server would just be a worse error message.
      return true
    }
  }

  if (to.meta.public) return true

  if (auth.status === 'signedOut') {
    return { path: '/sign-in', replace: true }
  }

  // Household is the owner's alone. A member who deep-links here gets the shelf,
  // not a 403 they can do nothing about.
  if (to.meta.owner && !auth.isOwner) return { path: '/', replace: true }

  return true
})
