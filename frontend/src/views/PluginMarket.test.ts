import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import PluginMarket from './PluginMarket.vue'
import { createMemoryHistory, createRouter } from 'vue-router'

const api = vi.hoisted(() => ({ market: vi.fn(), install: vi.fn() }))
vi.mock('../api/plugins', async original => ({ ...await original<typeof import('../api/plugins')>(), fetchPluginMarket: api.market, installMarketPlugin: api.install }))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.clearAllMocks() })
const entry = { id: 'zboard.oauth', name: 'OAuth for ZBoard', description: 'GitHub and Google login', publisher: 'higanbana986', surfaces: [], repository: 'https://github.com/higanbana986/zboard-oauth' }
async function render(discovery: boolean, path = '/admin/plugin-market', pending?: Promise<unknown>) {
  api.market.mockReturnValue(pending || Promise.resolve({ configured: true, kind: discovery ? 'registry' : 'signed', entries: [{ ...entry, discovery_only: discovery }], expires_at: '' }))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push(path); await router.isReady()
  wrapper = mount(PluginMarket, { global: { plugins: [router] } })
  await flushPromises()
  return wrapper
}
it('shows admitted OAuth without embedding a release version in the directory', async () => {
  const w = await render(true)
  expect(w.text()).toContain('OAuth for ZBoard')
  expect(w.text()).toContain('官方收录')
  expect(w.text()).not.toContain('vundefined')
  expect(w.get('a[href="/admin/plugin-market/zboard.oauth"]').text()).toBe('OAuth for ZBoard')
  expect(w.findAll('button').some(b => b.text() === '安装插件')).toBe(false)
  expect(api.install).not.toHaveBeenCalled()
})
it('restores capsule filters from the URL without fetching or installing another package', async () => {
  const w = await render(true, '/admin/plugin-market?q=missing&surface=admin')
  expect(w.text()).toContain('暂无匹配的插件')
  expect(w.find('.plugins-toolbar input').exists()).toBe(false)
  expect(api.market).toHaveBeenCalledOnce()
  expect(api.install).not.toHaveBeenCalled()
})
it('routes signed catalogs through the same detail and inspection flow', async () => {
  const w = await render(false)
  expect(w.text()).toContain('签名目录')
  expect(w.find('a[href="/admin/plugin-market/zboard.oauth"]').exists()).toBe(true)
})

it('keeps an unread directory unknown instead of reporting zero results', async () => {
  let reject!: (error: Error) => void
  const pending = new Promise((_, fail) => { reject = fail })
  const w = await render(true, '/admin/plugin-market', pending)
  expect(w.text()).not.toContain('0 个插件')
  expect(w.text()).not.toContain('暂无匹配的插件')
  expect(w.text()).toContain('正在加载市场目录')
  reject(new Error('unavailable')); await flushPromises()
  expect(w.text()).toContain('市场暂不可用')
  expect(w.text()).not.toContain('0 个插件')
  expect(w.text()).not.toContain('暂无匹配的插件')
})
it('retains a successfully empty directory while refreshing', async () => {
  const w = await render(true, '/admin/plugin-market', Promise.resolve({ configured: true, entries: [], expires_at: '' }))
  expect(w.text()).toContain('0 个插件')
  expect(w.text()).toContain('暂无匹配的插件')
  let resolve!: (value: unknown) => void
  api.market.mockReturnValueOnce(new Promise(done => { resolve = done }))
  await w.get('button[aria-label="刷新插件市场"]').trigger('click')
  expect(w.text()).not.toContain('正在加载市场目录')
  expect(w.text()).toContain('暂无匹配的插件')
  resolve({ configured: true, entries: [], expires_at: '' }); await flushPromises()
})
