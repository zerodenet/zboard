import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import AccountIntegrations from './AccountIntegrations.vue'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../../utils/reauthenticationDrafts'
import { fetchNavigation } from '../../api/menus'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../../api/menus', () => ({ fetchNavigation: vi.fn(async () => ({ page_available: true, nodes: [], revision: 1 })) }))
const mocks = vi.hoisted(() => ({ list: vi.fn(), issue: vi.fn(), revoke: vi.fn() }))
vi.mock('../../api/integrations', () => ({ listIntegrationCredentials: mocks.list, issueIntegrationCredential: mocks.issue, revokeIntegrationCredential: mocks.revoke }))
vi.mock('../../api/client', () => ({ API_BASE: '/api/v1' }))
const credential = { id: 7, name: '日报', token_prefix: 'zbi_prefix', scopes: ['metering.usage.query'], expires_at: '2099-01-01T00:00:00Z', revoked_at: null, created_at: '2026-09-13T00:00:00Z' }
let view: VueWrapper | undefined
const path = '/account/integrations'
async function open() {
 const router = createRouter({ history: createMemoryHistory(), routes: [{ path, component: AccountIntegrations }] })
 await router.push(path)
 view = mount(AccountIntegrations, { global: { plugins: [router], stubs: { RouterLink: true, ConfirmDialog: true } } })
 await flushPromises()
 return view
}
function stored() { return Array.from({ length: sessionStorage.length }, (_, i) => sessionStorage.getItem(sessionStorage.key(i)!)).join('') }
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
beforeEach(() => { vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts(); state.app = reactive({ token: 'verified', user: { id: 1 }, reauthRequired: false, sessionEpoch: 0 }); vi.mocked(fetchNavigation).mockResolvedValue({ page_available: true, nodes: [], revision: 1 } as any); mocks.list.mockResolvedValue([]); mocks.issue.mockResolvedValue({ credential, token: 'zbi_one_time_secret' }); mocks.revoke.mockResolvedValue({}) })
it('keeps the issued secret only until dismissed and revokes selected credentials', async () => {
 const w = await open()
 await flushPromises()
 await w.get('input[placeholder="例如：每日报表"]').setValue('日报')
 mocks.list.mockResolvedValue([credential])
 await w.get('form').trigger('submit'); await flushPromises()
 expect(mocks.issue).toHaveBeenCalledWith('日报', 30)
 expect((w.get('input[readonly]').element as HTMLInputElement).value).toBe('zbi_one_time_secret')
 expect(w.text()).toContain('本人流量查询')
 await w.findAll('button').find(b => b.text() === '已保存，关闭原文')!.trigger('click')
 expect(w.find('input[readonly]').exists()).toBe(false)
 await w.findAll('button').find(b => b.text() === '撤销')!.trigger('click')
 w.getComponent(ConfirmDialog).vm.$emit('confirm'); await flushPromises()
 expect(mocks.revoke).toHaveBeenCalledWith(7)
 expect(w.getComponent(ConfirmDialog).props('open')).toBe(false)
 w.unmount(); view = undefined
})

it('restores only ordinary name/days after same-account authentication without issuing or revoking', async () => {
 const w = await open(), vm = w.vm as any
 vm.name = 'Unsubmitted daily report'; vm.days = 90; vm.selected = credential
 expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: true, count: 1 })
 const payloadKey = Array.from({ length: sessionStorage.length }, (_, i) => sessionStorage.key(i)!).find(key => key.startsWith('zboard.reauth-draft.v1.'))!
 expect(JSON.parse(sessionStorage.getItem(payloadKey)!).data).toEqual({ name: 'Unsubmitted daily report', days: 90 })
 expect(stored()).not.toContain('metering.usage.query')
 w.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, false)
 const restored = await open(), form = restored.vm as any
 expect(form.name).toBe('Unsubmitted daily report'); expect(form.days).toBe(90)
 expect(form.selected).toBeNull(); expect(form.issuedToken).toBe('')
 expect(mocks.issue).not.toHaveBeenCalled(); expect(mocks.revoke).not.toHaveBeenCalled()
})
it('never captures an issued API token, scopes or a selected revoke operation', async () => {
 const w = await open(), vm = w.vm as any
 vm.name = 'Report'; vm.days = 365; vm.issuedToken = 'zbi_private_one_time'; vm.selected = credential
 expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: true, count: 0 })
 expect(stored()).toBe('')
 vm.issuedToken = ''
 expect(captureReauthenticationDrafts(1, path)).toMatchObject({ ok: true, count: 1 })
 expect(stored()).not.toContain('zbi_private_one_time'); expect(stored()).not.toContain('metering.usage.query'); expect(stored()).not.toContain('token_prefix')
 expect(mocks.issue).not.toHaveBeenCalled(); expect(mocks.revoke).not.toHaveBeenCalled()
})
it('does not recover old input for a different account', async () => {
 const w = await open(), vm = w.vm as any
 vm.name = 'Previous account report'; vm.days = 90
 expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
 state.app.user.id = 2; state.app.token = 'replacement'; state.app.sessionEpoch++
 confirmReauthenticationDraftIdentity(2, false)
 expect(vm.name).toBe(''); expect(vm.days).toBe(30)
 w.unmount(); view = undefined
 const next = await open()
 expect((next.vm as any).name).toBe(''); expect((next.vm as any).days).toBe(30)
 expect(stored()).toBe('')
 expect(mocks.issue).not.toHaveBeenCalled(); expect(mocks.revoke).not.toHaveBeenCalled()
})
it('requires fresh page permission and never stores extra authority fields', async () => {
 const w = await open(), vm = w.vm as any
 expect(vm.integrationDraft({ name: 'Report', days: 30, scopes: ['admin'], token: 'PRIVATE', selected: credential })).toEqual({ name: 'Report', days: 30 })
 expect(vm.integrationDraft({ name: 'Report', days: 0 })).toBeUndefined()
 expect(vm.integrationDraft({ name: '界'.repeat(161), days: 30 })).toBeUndefined()
 vm.name = 'Permission-bound report'
 expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
 w.unmount(); view = undefined; confirmReauthenticationDraftIdentity(1, false)
 vi.mocked(fetchNavigation).mockResolvedValue({ page_available: false, nodes: [], revision: 2 } as any)
 const next = await open()
 expect((next.vm as any).name).toBe(''); expect(stored()).toBe('')
 expect(mocks.issue).not.toHaveBeenCalled(); expect(mocks.revoke).not.toHaveBeenCalled()
})
