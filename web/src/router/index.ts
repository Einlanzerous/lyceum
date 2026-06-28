import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
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
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})
