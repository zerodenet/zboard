import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import PrimeVue from 'primevue/config'
import AccountIntegrations from './AccountIntegrations.vue'
import ConfirmDialog from '../../components/ConfirmDialog.vue'
const mocks = vi.hoisted(() => ({ list: vi.fn(), issue: vi.fn(), revoke: vi.fn() }))
vi.mock('../../api/integrations', () => ({ listIntegrationCredentials: mocks.list, issueIntegrationCredential: mocks.issue, revokeIntegrationCredential: mocks.revoke }))
vi.mock('../../api/client', () => ({ API_BASE: '/api/v1' }))
const credential = { id: 7, name: '日报', token_prefix: 'zbi_prefix', scopes: ['metering.usage.query'], expires_at: '2099-01-01T00:00:00Z', revoked_at: null, created_at: '2026-09-13T00:00:00Z' }
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue([]); mocks.issue.mockResolvedValue({ credential, token: 'zbi_one_time_secret' }); mocks.revoke.mockResolvedValue({}) })
it('keeps the issued secret only until dismissed and revokes selected credentials', async () => {
 const w = mount(AccountIntegrations, { global: { plugins: [PrimeVue], stubs: { RouterLink: true, ConfirmDialog: true } } })
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
 w.unmount()
})
