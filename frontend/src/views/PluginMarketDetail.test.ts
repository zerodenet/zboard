import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import PrimeVue from 'primevue/config'
import PluginMarketDetail from './PluginMarketDetail.vue'
import UiSelect from '../components/UiSelect.vue'
const api = vi.hoisted(() => ({ detail: vi.fn(), preview: vi.fn(), install: vi.fn() }))
vi.mock('../api/plugins', async original => ({ ...await original<typeof import('../api/plugins')>(), fetchMarketDetail: api.detail, previewMarketPlugin: api.preview, confirmMarketPlugin: api.install }))
const modal = defineComponent({ props: ['open'], template: '<section v-if="open" role="dialog"><slot/><slot name="footer"/></section>' })
let wrapper: ReturnType<typeof mount>
const detail = (id = 'zboard.oauth') => ({ entry: { id, name: id, publisher: 'publisher', repository: 'https://github.com/example/plugin' }, platform: 'linux-amd64', release: { version: '0.0.1', artifacts: [{ platform: 'linux-amd64', url: 'https://github.com/example/plugin/releases/download/v0.0.1/linux.zbplugin', sha256: 'digest', size: 1024 }, { platform: 'darwin-arm64', url: 'https://github.com/example/plugin/releases/download/v0.0.1/mac.zbplugin', sha256: 'mac', size: 2048 }] } })
beforeEach(() => {
 vi.resetAllMocks()
 api.detail.mockImplementation(async id => detail(id))
 api.preview.mockResolvedValue({ manifest: { id: 'zboard.oauth', name: 'OAuth', version: '0.0.1', surfaces: ['admin'], capabilities: ['zboard.identity.provider.v1'] }, publisher: 'publisher', fingerprint: 'fingerprint', digest: 'digest', trusted: false, compatibility: { compatible: true, tested: false } })
 api.install.mockResolvedValue({ id: 'zboard.oauth' })
})
afterEach(() => wrapper?.unmount())
async function render() {
 const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/plugin-market', component: { template: '<p>市场</p>' } }, { path: '/admin/plugin-market/:pluginId', component: PluginMarketDetail }, { path: '/admin/plugins/:pluginId', component: { template: '<p>插件管理</p>' } }] })
 await router.push('/admin/plugin-market/zboard.oauth'); await router.isReady()
 wrapper = mount({ template: '<RouterView />' }, { global: { plugins: [router, PrimeVue], stubs: { ModalDialog: modal } } }); await flushPromises()
 return router
}
async function click(label: string) { const b = wrapper.findAll('button').find(b => b.text() === label)!; await b.trigger('click'); await flushPromises() }
it('offers platform downloads and installs only after package inspection and explicit source trust', async () => {
 const router = await render()
 expect(wrapper.get('a[href$="linux.zbplugin"]').text()).toContain('下载')
 wrapper.getComponent(UiSelect).vm.$emit('update:modelValue', 'darwin-arm64'); await flushPromises()
 expect(wrapper.find('a[href$="mac.zbplugin"]').exists()).toBe(true)
 expect(api.preview).not.toHaveBeenCalled()
 await click('在线安装')
 expect(api.preview).toHaveBeenCalledWith('zboard.oauth', expect.any(AbortSignal))
 expect(api.install).not.toHaveBeenCalled()
 expect(wrapper.text()).toContain('fingerprint')
 expect(wrapper.findAll('button').find(b => b.text() === '确认安装')!.attributes('disabled')).toBeDefined()
 await wrapper.get('input[type="checkbox"]').setValue(true)
 await click('确认安装')
 expect(api.install).toHaveBeenCalledWith('zboard.oauth', expect.objectContaining({ digest: 'digest', fingerprint: 'fingerprint' }), true)
 expect(router.currentRoute.value.path).toBe('/admin/plugins/zboard.oauth')
})
it('links an installed package to management and avoids installing the identical package again', async () => {
 api.detail.mockResolvedValue({ ...detail(), installed: { version: '0.0.1', digest: 'digest', state: 'disabled' } })
 await render()
 expect(wrapper.get('a[href="/admin/plugins/zboard.oauth"]').text()).toBe('管理插件')
 expect(wrapper.findAll('button').find(b => b.text() === '当前版本已安装')!.attributes('disabled')).toBeDefined()
})
it('keeps unavailable releases explicit and does not expose an install action', async () => {
 api.detail.mockResolvedValue({ ...detail(), release: undefined, notice: '发行信息暂不可用' })
 await render(); expect(wrapper.text()).toContain('发行信息暂不可用')
 expect(wrapper.findAll('button').some(b => b.text() === '在线安装')).toBe(false)
})
it('drops stale previews and consent when navigation selects another plugin', async () => {
 let done!: (v: any) => void
 api.preview.mockReturnValue(new Promise(resolve => { done = resolve }))
 const router = await render(); await click('在线安装')
 await router.push('/admin/plugin-market/second'); await flushPromises()
 done({ manifest: { name: 'STALE SECRET' } }); await flushPromises()
 expect(wrapper.text()).not.toContain('STALE SECRET')
 expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
 expect(api.install).not.toHaveBeenCalled()
})
it('keeps incompatible packages blocked even after source confirmation', async () => {
 const preview = await api.preview()
 api.preview.mockResolvedValue({ ...preview, compatibility: { compatible: false, tested: false, reason: '插件协议版本不受支持' } })
 await render(); await click('在线安装')
 await wrapper.get('input[type="checkbox"]').setValue(true)
 expect(wrapper.text()).toContain('插件协议版本不受支持')
 expect(wrapper.findAll('button').find(b => b.text() === '确认安装')!.attributes('disabled')).toBeDefined()
 expect(api.install).not.toHaveBeenCalled()
})

it('allows installation when the publisher host version declaration is only advisory', async () => {
 const preview = await api.preview()
 api.preview.mockResolvedValue({ ...preview, trusted: true, compatibility: { compatible: true, tested: false, reason: '', warning: '发行版本声明仅供参考' } })
 await render(); await click('在线安装')
 expect(wrapper.text()).toContain('发行版本声明仅供参考')
 expect(wrapper.findAll('button').find(b => b.text() === '确认安装')!.attributes('disabled')).toBeUndefined()
 await click('确认安装')
 expect(api.install).toHaveBeenCalledOnce()
})
