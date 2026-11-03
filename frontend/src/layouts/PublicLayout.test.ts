import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { buildSiteProfile } from '../utils/siteProfile'
import PublicLayout from './PublicLayout.vue'

const app = vi.hoisted(() => ({ isAuthenticated: false, isAdmin: false, installation: { allow_registration: true }, siteProfile: {} as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => app }))
vi.mock('../api/menus', async () => {
  const { menuFixture } = await import('../test/menuFixtures')
  return { fetchNavigation: vi.fn(async () => menuFixture('public')) }
})
let wrapper: VueWrapper | undefined
beforeEach(() => { app.isAuthenticated = false; app.isAdmin = false; app.installation.allow_registration = true; app.siteProfile = buildSiteProfile([], '预览站点') })
afterEach(() => { wrapper?.unmount(); wrapper = undefined })
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<p>页面</p>' } }] })
  await router.push('/'); await router.isReady()
  wrapper = mount(PublicLayout, { global: { plugins: [router] } })
  await flushPromises()
  return router
}
it.each([false, true])('keeps login and registration as separate links when registration is %s', async allowed => {
  app.installation.allow_registration = allowed
  const router = await open()
  expect(wrapper!.get('.public-actions a[href="/login"]').text()).toBe('登录')
  expect(wrapper!.find('.public-actions a[href="/register"]').exists()).toBe(allowed)
  expect(wrapper!.find('button a').exists()).toBe(false)
  await wrapper!.get('.public-actions a[href="/login"]').trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/login')
})
it.each([false, true])('opens the authenticated landing page with administrator=%s', async admin => {
  app.isAuthenticated = true; app.isAdmin = admin
  const router = await open(), destination = admin ? '/admin/dashboard' : '/account'
  expect(wrapper!.findAll('.public-actions a')).toHaveLength(1)
  await wrapper!.get(`.public-actions a[href="${destination}"]`).trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe(destination)
})
