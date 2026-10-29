import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import PluginConfigDialog from './PluginConfigDialog.vue'
import { fetchPluginConfig, savePluginConfig } from '../api/plugins'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, registerReauthenticationDraft } from '../utils/reauthenticationDrafts'
vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }) }))
vi.mock('../api/plugins', () => ({ fetchPluginConfig: vi.fn(), savePluginConfig: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn() }))
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined, unregister: (() => void) | undefined
const path = '/admin/plugins/example/configuration'
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts()
  vi.mocked(fetchPluginConfig).mockResolvedValue({ configured: true, revision: 7 } as any)
})
afterEach(() => { view?.unmount(); view = undefined; unregister?.(); unregister = undefined; clearReauthenticationDrafts() })
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component: PluginConfigDialog }] })
  await router.push(path)
  view = shallowMount(PluginConfigDialog, { props: { pluginId: 'example' }, global: { plugins: [router], renderStubDefaultSlot: true, stubs: { ModalDialog: modal, PageAlert: { props: ['title'], template: '<aside><strong>{{title}}</strong><slot /></aside>' } } } })
  await flushPromises()
  return view
}
it('blocks external draft capture while arbitrary JSON is dirty without saving or exposing any JSON to storage', async () => {
  const v = await open(), vm = v.vm as any
  const text = '{"token":"DO-NOT-STORE","nested":{"private_key":"DO-NOT-STORE"},"ordinary":"retain original"}'
  vm.text = text; await flushPromises()
  expect(v.text()).toContain('插件配置不能暂存到第三方登录页')
  const result = captureReauthenticationDrafts(1, path)
  expect(result.ok).toBe(false); expect(vm.text).toBe(text); expect(v.find('form').exists()).toBe(true); expect(sessionStorage.length).toBe(0); expect(savePluginConfig).not.toHaveBeenCalled()
})
it('does not parse invalid JSON while deciding whether capture must be blocked', async () => {
  const v = await open(), vm = v.vm as any
  vm.text = '{BROKEN JSON DO-NOT-STORE'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false); expect(vm.text).toBe('{BROKEN JSON DO-NOT-STORE'); expect(sessionStorage.length).toBe(0); expect(savePluginConfig).not.toHaveBeenCalled()
})
it('prevents partial capture of another ordinary form when the dirty plugin JSON cannot be preserved', async () => {
  unregister = registerReauthenticationDraft({ actorID: 1, permission: 'admin', scope: 'admin.other-ordinary', route: () => path, read: () => ({ name: 'Ordinary local edit' }), sanitize: value => value as { name: string } })
  const v = await open(); (v.vm as any).text = '{"secret":"DO-NOT-STORE"}'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false); expect(sessionStorage.length).toBe(0); expect((v.vm as any).text).toContain('DO-NOT-STORE'); expect(savePluginConfig).not.toHaveBeenCalled()
})
it('does not block ordinary authentication while the open JSON editor is clean', async () => {
  const v = await open()
  expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: true, count: 0 }); expect(v.text()).not.toContain('插件配置不能暂存到第三方登录页'); expect(savePluginConfig).not.toHaveBeenCalled()
})
it('stops blocking after an explicit discard and component close', async () => {
  const v = await open(); (v.vm as any).text = '{"secret":"DO-NOT-STORE"}'
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  v.unmount(); view = undefined
  expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: true, count: 0 }); expect(savePluginConfig).not.toHaveBeenCalled()
})
it('does not block after clearing the unsaved JSON back to the clean baseline', async () => {
  const v = await open(), vm = v.vm as any
  vm.text = '{"ordinary":true}'; expect(captureReauthenticationDrafts(1, path).ok).toBe(false)
  vm.text = ''; await flushPromises()
  expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: true, count: 0 }); expect(savePluginConfig).not.toHaveBeenCalled()
})
