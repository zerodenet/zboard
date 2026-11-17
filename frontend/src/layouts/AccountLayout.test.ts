import { mount, flushPromises, DOMWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi, afterEach } from 'vitest'
import { menuFixture } from '../test/menuFixtures'
import AccountLayout from './AccountLayout.vue'

vi.mock('../stores/app', () => ({ useAppStore: () => ({ siteName: '狗梯', siteProfile: {}, user: { email: 'a-long-account@example.test' }, isAdmin: true, announcementUnreadCount: 2, clear: vi.fn() }) }))
vi.mock('../api/menus', async () => {
  const { menuFixture } = await import('../test/menuFixtures')
  return { fetchNavigation: vi.fn(async () => { const value = menuFixture('account'); value.nodes.push({ ...value.nodes[0], id: 'plugin:example', parent_id: '', label: '扩展入口', path: '/account/extensions/example/home', owner: 'plugin' }); return value }) }
})

afterEach(() => { document.body.innerHTML = '' })

it('keeps every navigation entry available and closes the menu on navigation and Escape', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<p>页面</p>' } }] })
  await router.push('/account')
  const wrapper = mount(AccountLayout, { attachTo: document.body, global: { plugins: [router], stubs: { AccountCleanupNotice: true } } })
  await flushPromises()
  const toggle = wrapper.get('.account-menu')
  const discovered = new Set(wrapper.findAll('#account-navigation a').map(link => link.attributes('href')))
  expect(discovered.size).toBeLessThan(menuFixture('account').nodes.filter(node => node.path).length)
  for (const trigger of wrapper.findAll('#account-navigation button[aria-haspopup="menu"]')) {
    await trigger.trigger('keydown', { key: 'ArrowDown' }); await flushPromises()
    const menu = new DOMWrapper(document.body.querySelector('[role="menu"]')!)
    for (const link of menu.findAll('a')) discovered.add(link.attributes('href'))
    await menu.trigger('keydown', { key: 'Escape' }); await flushPromises()
  }
  expect([...discovered].sort()).toEqual([...menuFixture('account').nodes.filter(node => node.path).map(node => node.path), '/account/extensions/example/home'].sort())
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
