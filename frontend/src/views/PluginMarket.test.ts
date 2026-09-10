import { mount, flushPromises } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, expect, it, vi } from 'vitest'
import PluginMarket from './PluginMarket.vue'

const api = vi.hoisted(() => ({ market: vi.fn(), install: vi.fn() }))
vi.mock('../api/plugins', async original => ({ ...await original<typeof import('../api/plugins')>(), fetchPluginMarket: api.market, installMarketPlugin: api.install }))
let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount(); vi.clearAllMocks() })
const entry = { id: 'zboard.oauth', name: 'OAuth for ZBoard', description: 'GitHub and Google login', version: '0.0.1', publisher: 'higanbana986', surfaces: [], repository: 'https://github.com/higanbana986/zboard-oauth' }
async function render(discovery: boolean) {
  api.market.mockResolvedValue({ configured: true, kind: discovery ? 'registry' : 'signed', entries: [{ ...entry, discovery_only: discovery }], expires_at: '' })
  wrapper = mount(PluginMarket, { global: { plugins: [PrimeVue], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } } })
  await flushPromises()
  return wrapper
}
it('shows source-only OAuth from the public registry without offering trusted installation', async () => {
  const w = await render(true)
  expect(w.text()).toContain('OAuth for ZBoard')
  expect(w.text()).toContain('公开收录')
  expect(w.get('a[href="/admin/plugin-market/zboard.oauth"]').text()).toContain('下载 / 安装')
  expect(w.findAll('button').some(b => b.text() === '安装插件')).toBe(false)
  expect(api.install).not.toHaveBeenCalled()
})
it('routes signed catalogs through the same detail and inspection flow', async () => {
  const w = await render(false)
  expect(w.text()).toContain('签名目录')
  expect(w.find('a[href="/admin/plugin-market/zboard.oauth"]').exists()).toBe(true)
})
