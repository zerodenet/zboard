import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Plugins from '../views/Plugins.vue'
import PluginDetail from '../views/PluginDetail.vue'
import PluginConfiguration from '../views/PluginConfiguration.vue'
import PluginImportDialog from './PluginImportDialog.vue'
import PluginConfigDialog from './PluginConfigDialog.vue'
import UiFileUpload from '../components/UiFileUpload.vue'
import type { Plugin } from '../api/plugins'
const mocks = vi.hoisted(() => ({ list: vi.fn(), config: vi.fn(), operations: vi.fn(), import: vi.fn(), preview: vi.fn(), save: vi.fn(), action: vi.fn(), confirm: vi.fn(), migrations: vi.fn() }))
vi.mock('../api/plugins', async importOriginal => ({ ...await importOriginal<typeof import('../api/plugins')>(), fetchPlugins: mocks.list, fetchPluginConfig: mocks.config, fetchPluginOperations: mocks.operations, importPlugin: mocks.import, previewPlugin: mocks.preview, savePluginConfig: mocks.save, pluginAction: mocks.action, fetchPluginMigrations: mocks.migrations }))
vi.mock('../utils/feedback', async importOriginal => ({ ...await importOriginal<typeof import('../utils/feedback')>(), confirmAction: mocks.confirm }))
const plugin = (id = 'zboard.oauth'): Plugin => ({
  admission: { accepted: true, capabilities: ['zboard.ui.page.v1', 'zboard.config.v1'] }, data: { version: 0, target_version: 0, epoch: 0, revision: 0, stored: false, compatible: true, migration_required: false },
  id, name: id === 'zboard.oauth' ? 'OAuth 登录' : '第二个插件', version: '0.2.0', version_id: 'v2', digest: 'digest', publisher: 'publisher', state: 'disabled', enabled: false, generation: 3, config_revision: 7, last_error: '',
  manifest: { surfaces: ['public', 'admin'], description: '第三方账号登录', capabilities: ['zboard.ui.page.v1', 'zboard.config.v1'], components: { server: {} }, contributions: { pages: [{ id: 'configuration', surface: 'admin', title: '配置', purpose: 'configuration' }] } },
  compatibility: { compatible: true, tested: true, reason: '' }, versions: [{ id: 'v2', version: '0.2.0', digest: 'digest', created_at: '2026-09-09' }, { id: 'v1', version: '0.1.0', digest: 'old-digest', created_at: '2026-09-08' }],
})
const ModalStub = defineComponent({ props: ['open'], emits: ['close'], template: '<section v-if="open" role="dialog"><slot/><slot name="footer" :request-close="() => $emit(\'close\')"/></section>' })
const mounted: Array<{ unmount: () => void }> = []
async function render(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/admin/plugins', component: Plugins },
    { path: '/admin/plugin-market', component: { template: '<div>市场</div>' } },
    { path: '/admin/plugins/:pluginId', component: PluginDetail },
    { path: '/admin/plugins/:pluginId/configuration', component: PluginConfiguration },
  ] })
  await router.push(path); await router.isReady()
  const wrapper = mount({ template: '<RouterView />' }, { attachTo: document.body, global: { plugins: [router, PrimeVue], stubs: { ModalDialog: ModalStub, PluginFrame: { template: '<iframe title="插件配置页面" />' } } } })
  mounted.push(wrapper); await flushPromises()
  return { wrapper, router }
}
const clickText = async (wrapper: ReturnType<typeof mount>, label: string) => {
  const button = wrapper.findAll('button').find(b => b.text() === label)
  expect(button, label).toBeDefined(); await button!.trigger('click'); await flushPromises()
}
beforeEach(() => {
  mocks.list.mockResolvedValue([plugin()]); mocks.config.mockResolvedValue({ configured: true, revision: 7 }); mocks.operations.mockResolvedValue([])
  mocks.preview.mockResolvedValue({ manifest: { ...plugin().manifest, id: plugin().id, name: plugin().name, version: plugin().version }, publisher: 'publisher', public_key: 'key', fingerprint: 'fingerprint', digest: 'digest', trusted: true, compatibility: plugin().compatibility }); mocks.import.mockResolvedValue(plugin()); mocks.save.mockResolvedValue({ revision: 8 }); mocks.confirm.mockResolvedValue(true); mocks.action.mockResolvedValue(plugin()); mocks.migrations.mockResolvedValue([])
})
afterEach(() => { mounted.splice(0).forEach(w => w.unmount()); document.body.innerHTML = '' })
describe('plugin management navigation', () => {
  it('keeps configuration out of the list and loads operations only in their detail section', async () => {
    const { wrapper, router } = await render('/admin/plugins?q=OAuth&surface=admin')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(mocks.config).not.toHaveBeenCalled(); expect(mocks.operations).not.toHaveBeenCalled()
    await wrapper.get('a[href="/admin/plugins/zboard.oauth"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('关于插件'); expect(wrapper.text()).not.toContain('恢复此版本')
    expect(mocks.operations).not.toHaveBeenCalled()
    await router.push('/admin/plugins/zboard.oauth?tab=operations'); await flushPromises()
    expect(mocks.operations).toHaveBeenCalledWith('zboard.oauth', expect.any(AbortSignal))
    await router.push('/admin/plugins/zboard.oauth/configuration'); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(true)
    expect(wrapper.find('textarea').exists()).toBe(false)
  })
  it('selects a package without importing and keeps a failed import open for retry', async () => {
    const { wrapper } = await render('/admin/plugins')
    await clickText(wrapper, '离线导入')
    const dialog = wrapper.getComponent(PluginImportDialog)
    const file = new File(['package'], 'oauth.zbplugin')
    dialog.getComponent(UiFileUpload).vm.$emit('select', [file]); await flushPromises()
    expect(mocks.import).not.toHaveBeenCalled(); expect(wrapper.text()).toContain('oauth.zbplugin')
    mocks.import.mockRejectedValueOnce({ response: { data: { message: '发布者签名不受信任' } } })
    await clickText(wrapper, '查看插件'); await clickText(wrapper, '确认导入')
    expect(wrapper.text()).toContain('发布者签名不受信任'); expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    await clickText(wrapper, '取消'); expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await clickText(wrapper, '离线导入'); expect(wrapper.text()).not.toContain('oauth.zbplugin')
    expect(mocks.import).toHaveBeenCalledTimes(1)
  })
  it('opens the imported plugin detail and leaves activation as an explicit action', async () => {
    const { wrapper, router } = await render('/admin/plugins')
    await clickText(wrapper, '离线导入')
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [new File(['package'], 'oauth.zbplugin')]); await flushPromises()
    await clickText(wrapper, '查看插件'); await clickText(wrapper, '确认导入')
    expect(router.currentRoute.value.path).toBe('/admin/plugins/zboard.oauth')
    expect(wrapper.text()).toContain('系统已完成能力校验和数据准备'); expect(mocks.action).not.toHaveBeenCalled()
  })
  it('requires explicit trust for an unknown key and clears consent on package replacement', async () => {
    mocks.preview.mockResolvedValueOnce({ ...await mocks.preview(), trusted: false })
    const { wrapper } = await render('/admin/plugins')
    await clickText(wrapper, '离线导入')
    const file = new File(['package'], 'oauth.zbplugin')
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [file]); await flushPromises()
    await clickText(wrapper, '查看插件')
    expect(mocks.import).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').find(b => b.text() === '确认导入')!.attributes('disabled')).toBeDefined()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    mocks.preview.mockResolvedValueOnce({ ...await mocks.preview(), trusted: false })
    wrapper.getComponent(UiFileUpload).vm.$emit('select', [file]); await flushPromises()
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    await clickText(wrapper, '查看插件')
    expect((wrapper.get('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await clickText(wrapper, '确认导入')
    expect(mocks.import).toHaveBeenCalledWith(file, expect.objectContaining({ fingerprint: 'fingerprint', digest: 'digest' }), true)
  })
  it('ignores stale detail and operation responses after switching plugins', async () => {
    const { wrapper, router } = await render('/admin/plugins/zboard.oauth?tab=operations')
    let finish!: (value: Plugin[]) => void
    let finishOps!: (value: unknown[]) => void
    mocks.list.mockReturnValueOnce(new Promise<Plugin[]>(resolve => { finish = resolve }))
    mocks.operations.mockReturnValueOnce(new Promise(resolve => { finishOps = resolve }))
    await router.push('/admin/plugins/slow?tab=operations'); await flushPromises()
    mocks.list.mockResolvedValueOnce([plugin('second')])
    await router.push('/admin/plugins/second?tab=operations'); await flushPromises()
    finish([plugin('slow')]); finishOps([{ id: 'old', action: 'OLD OPERATION', state: 'succeeded' }]); await flushPromises()
    expect(wrapper.text()).toContain('第二个插件'); expect(wrapper.text()).not.toContain('slow'); expect(wrapper.text()).not.toContain('OLD OPERATION')
  })
  it('reads a fresh revision for advanced configuration and validates before saving', async () => {
    const { wrapper } = await render('/admin/plugins/zboard.oauth/configuration')
    await clickText(wrapper, '高级 JSON 配置')
    expect(wrapper.get('iframe').isVisible()).toBe(false)
    const dialog = wrapper.getComponent(PluginConfigDialog)
    await dialog.get('textarea').setValue('[]'); await dialog.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save).not.toHaveBeenCalled(); expect(dialog.text()).toContain('请输入有效的 JSON 对象')
    await dialog.get('textarea').setValue('{"enabled":true}'); await dialog.get('form').trigger('submit'); await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith('zboard.oauth', 7, { enabled: true })
    expect(wrapper.findComponent(PluginConfigDialog).exists()).toBe(false); expect(wrapper.find('iframe').exists()).toBe(true)
  })
  it('pages recent operations without appending a long history to the detail', async () => {
    mocks.operations.mockResolvedValue(Array.from({ length: 21 }, (_, i) => ({ id: String(i), action: 'test', state: 'succeeded', actor: 'admin', message: `Record ${i}`, created_at: '2026-09-09' })))
    const { wrapper } = await render('/admin/plugins/zboard.oauth?tab=operations')
    expect(wrapper.findAll('.plugin-history')).toHaveLength(10)
    expect(wrapper.text()).not.toContain('Record 10')
    await clickText(wrapper, '下一页')
    expect(wrapper.text()).toContain('Record 10'); expect(wrapper.text()).not.toContain('Record 0')
    await clickText(wrapper, '下一页')
    expect(wrapper.findAll('.plugin-history')).toHaveLength(1)
  })
  it('uses completed host admission without an administrator permission workflow', async () => {
    const { wrapper } = await render('/admin/plugins/zboard.oauth')
    expect(wrapper.findAll('button').find(b => b.text() === '启用插件')!.attributes('disabled')).toBeUndefined()
    expect(wrapper.find('a[href$="/configuration"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('能力授权')
    expect(wrapper.text()).toContain('由 ZBoard 根据插件声明、调用身份和站点策略约束')
    await clickText(wrapper, '启用插件')
    expect(mocks.action).toHaveBeenCalledWith(plugin(), 'enable', false, '')
  })
  it('shows host migration status without exposing a manual migration action', async () => {
    const migrating = plugin()
    migrating.data = { ...migrating.data, target_version: 1, compatible: false, migration_required: true }
    mocks.list.mockResolvedValue([migrating])
    const { wrapper } = await render('/admin/plugins/zboard.oauth?tab=versions')
    expect(wrapper.text()).toContain('数据准备尚未完成')
    expect(wrapper.text()).toContain('重新导入插件时会自动重试')
    expect(wrapper.text()).not.toContain('执行数据迁移')
    expect(mocks.migrations).toHaveBeenCalled()
    expect(mocks.action).not.toHaveBeenCalled()
    expect(wrapper.findAll('button').find(b => b.text() === '启用插件')!.attributes('disabled')).toBeDefined()
  })
  it('lets the host stop a running plugin as part of uninstall', async () => {
    const active = { ...plugin(), enabled: true, state: 'active' }
    mocks.list.mockResolvedValue([active])
    const { wrapper } = await render('/admin/plugins/zboard.oauth')
    await clickText(wrapper, '卸载插件')
    expect(mocks.action).toHaveBeenCalledWith(active, 'uninstall', false, '')
  })
  it('protects a dirty advanced configuration from switching to a different plugin', async () => {
    const { wrapper, router } = await render('/admin/plugins/zboard.oauth/configuration')
    await clickText(wrapper, '高级 JSON 配置'); await wrapper.get('textarea').setValue('{"draft":true}')
    mocks.confirm.mockResolvedValueOnce(false)
    await router.push('/admin/plugins/second/configuration'); await flushPromises()
    expect(router.currentRoute.value.params.pluginId).toBe('zboard.oauth'); expect(wrapper.get('textarea').element.value).toContain('draft')
  })
})
