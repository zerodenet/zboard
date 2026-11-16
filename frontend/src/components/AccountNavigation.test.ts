import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it } from 'vitest'
import AccountNavigation from './AccountNavigation.vue'
import { navigationState } from '../stores/navigation'
import { menuFixture } from '../test/menuFixtures'
import type { MenuNode } from '../api/menus'

describe('account navigation single group trigger', () => {
  let wrapper: VueWrapper | undefined
  afterEach(() => {
    wrapper?.unmount()
    navigationState.account.snapshot.value = { revision: 0, nodes: [] }
  })
  async function open(path = '/account/plans', extra: MenuNode[] = []) {
    const snapshot = menuFixture('account')
    navigationState.account.snapshot.value = { ...snapshot, nodes: [...snapshot.nodes, ...extra] }
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
    ] })
    await router.push(path)
    await router.isReady()
    wrapper = mount(AccountNavigation, { attachTo: document.body, global: { plugins: [createPinia(), router] } })
    await flushPromises()
    return router
  }
  async function expand(label = '我的服务') {
    await wrapper!.get(`button[aria-label="${label}"]`).trigger('click')
    await flushPromises()
    return document.querySelector<HTMLElement>('[role="menu"]')!
  }
  it('uses one named trigger for the whole group and keeps the primary page in its menu', async () => {
    const router = await open()
    expect(wrapper!.find('a[href="/account"]').exists()).toBe(false)
    expect(wrapper!.findAll('button[aria-label="我的服务"]')).toHaveLength(1)
    expect(wrapper!.get('button[aria-label="我的服务"]').text()).toBe('我的服务')
    const menu = await expand()
    expect([...menu.querySelectorAll('a')].map(link => link.textContent?.trim())).toEqual(['我的服务', '连接设备', '流量记录'])
    menu.querySelector<HTMLAnchorElement>('a[href="/account"]')!.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/account')
    expect(wrapper!.emitted('selectPage')).toHaveLength(1)
  })
  it('identifies the active group and exact child instead of marking two separate controls', async () => {
    await open('/account/subscription')
    expect(wrapper!.get('button[aria-label="我的服务"]').attributes('aria-current')).toBe('true')
    const menu = await expand()
    const active = menu.querySelectorAll('[aria-current="page"]')
    expect(active).toHaveLength(1)
    expect(active[0]!.getAttribute('href')).toBe('/account/subscription')
  })
  it('uses backend labels and includes plugin pages without a hardcoded menu whitelist', async () => {
    await open('/account/plans', [{ id: 'plugin:daily', parent_id: 'account.services', surface: 'account', label: '服务检查', icon: '', path: '/account/plugins/daily', position: 100, hidden: false, owner: 'plugin', plugin_id: 'daily', page_id: 'daily', condition: '' }])
    const menu = await expand()
    expect(menu.querySelector('a[href="/account/plugins/daily"]')?.textContent).toContain('服务检查')
  })
  it('keeps single-page groups as direct links without an extra dropdown', async () => {
    const group: MenuNode = { id: 'custom:group', parent_id: '', surface: 'account', label: '客户工具', icon: '', path: '', position: 100, hidden: false, owner: 'custom', plugin_id: '', page_id: '', condition: '' }
    await open('/account/plans', [group, { ...group, id: 'custom:page', parent_id: group.id, label: '工具首页', path: '/account/tools' }])
    expect(wrapper!.get('a[href="/account/tools"]').text()).toBe('客户工具')
    expect(wrapper!.find('button[aria-label="客户工具"]').exists()).toBe(false)
  })
})
