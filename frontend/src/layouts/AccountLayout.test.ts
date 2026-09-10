import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import AccountLayout from './AccountLayout.vue'

vi.mock('../stores/app', () => ({ useAppStore: () => ({ siteName: '狗梯', siteProfile: {}, user: { email: 'a-long-account@example.test' }, isAdmin: true, announcementUnreadCount: 2, clear: vi.fn() }) }))
vi.mock('../plugins/PluginNavigation.vue', () => ({ default: { template: '<a href="/account/plugin">扩展入口</a>' } }))

it('keeps every navigation entry available and closes the menu on navigation and Escape', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<p>页面</p>' } }] })
  await router.push('/account')
  const wrapper = mount(AccountLayout, { global: { plugins: [router] } })
  const toggle = wrapper.get('.account-menu')
  expect(wrapper.findAll('nav a')).toHaveLength(10)
  await toggle.trigger('click')
  expect(toggle.attributes('aria-expanded')).toBe('true')
  await wrapper.get('header').trigger('keydown', { key: 'Escape' })
  expect(toggle.attributes('aria-expanded')).toBe('false')
  await toggle.trigger('click')
  await router.push('/account/subscription')
  await flushPromises()
  expect(toggle.attributes('aria-expanded')).toBe('false')
  expect(wrapper.get('nav').classes()).not.toContain('open')
  expect(wrapper.get('.account-identity').text()).toContain('a-long-account@example.test')
  wrapper.unmount()
})
