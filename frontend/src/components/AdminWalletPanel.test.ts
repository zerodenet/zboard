import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminWalletPanel from './AdminWalletPanel.vue'
import { debitWallet, fetchAdministrativeWallet, fetchWalletAdjustments, fetchWalletAdjustment, recordWalletPayout, reverseWalletAdjustment } from '../api/walletAdjustments'
import UiSelect from './UiSelect.vue'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/walletAdjustments', () => ({ debitWallet: vi.fn(), fetchAdministrativeWallet: vi.fn(), fetchWalletAdjustments: vi.fn(), fetchWalletAdjustment: vi.fn(), recordWalletPayout: vi.fn(), reverseWalletAdjustment: vi.fn() }))
const row = { id: 9, user_id: 27, currency: 'CNY', amount_minor: '900', reason: '工单特殊出款', ticket_id: 12, actor_id: 1, entry_id: 6, external_status: 'pending', created_at: '2026-10-19T00:00:00Z' }
function open() { return mount(AdminWalletPanel, { props: { userId: 27 } }) }
async function fillDebit(wrapper: ReturnType<typeof open>) { const form = wrapper.get('form[data-test="debit"]'); await form.findAll('input')[0]!.setValue('9.00'); await form.findAll('input')[1]!.setValue('12'); await form.findAll('input')[2]!.setValue('工单特殊出款'); return form }
beforeEach(() => {
 vi.clearAllMocks(); localStorage.clear(); state.app = reactive({ user: { id: 1 } })
 vi.mocked(fetchAdministrativeWallet).mockResolvedValue({ currency: 'CNY', balance_minor: '1000', reserved_minor: '100', available_minor: '900', entries: [], has_more: false })
 vi.mocked(fetchWalletAdjustments).mockResolvedValue({ items: [row], has_more: false }); vi.mocked(fetchWalletAdjustment).mockResolvedValue(row)
 vi.mocked(debitWallet).mockResolvedValue(row); vi.mocked(recordWalletPayout).mockResolvedValue(row)
})
describe('controlled wallet adjustment', () => {
 it('requires explicit debit and preserves the original ticketed request through a lost response and reload', async () => {
   const wrapper = open(); await flushPromises(); expect(wrapper.text()).toContain('已预留 CNY 1.00'); expect(debitWallet).not.toHaveBeenCalled()
   vi.mocked(debitWallet).mockRejectedValueOnce(new Error('response lost')); const form = await fillDebit(wrapper); await form.trigger('submit'); await flushPromises()
   const original = vi.mocked(debitWallet).mock.calls[0]![0]; expect(original).toEqual({ user_id: 27, currency: 'CNY', amount_minor: 900, ticket_id: 12, reason: '工单特殊出款', idempotency_key: expect.stringMatching(/^[a-f0-9]{32}$/) })
   expect(form.findAll('input').every(input => input.attributes('disabled') !== undefined)).toBe(true)
   await wrapper.findAll('button').find(button => button.text() === '刷新记录')!.trigger('click'); await flushPromises(); expect(debitWallet).toHaveBeenCalledTimes(1)
   wrapper.unmount(); const restored = open(); await flushPromises(); expect(debitWallet).toHaveBeenCalledTimes(1)
   await restored.get('form[data-test="debit"]').trigger('submit'); await flushPromises(); expect(debitWallet).toHaveBeenLastCalledWith(original); expect(debitWallet).toHaveBeenCalledTimes(2); restored.unmount()
 })
 it('records manual payout results separately and preserves timestamp and identity on explicit retry', async () => {
   const wrapper = open(); await flushPromises(); await wrapper.findAll('button').find(button => button.text() === '核对这笔出款')!.trigger('click'); await flushPromises()
   wrapper.getComponent(UiSelect).vm.$emit('update:modelValue','failed'); const form = wrapper.get('form[data-test="payout"]'); await form.findAll('input')[0]!.setValue('bank-private-reference'); await form.findAll('input')[1]!.setValue('人工查询确认未出款')
   vi.mocked(recordWalletPayout).mockRejectedValueOnce(new Error('response lost')); await form.trigger('submit'); await flushPromises(); const original = vi.mocked(recordWalletPayout).mock.calls[0]![1]
   expect(original.outcome).toBe('failed'); expect(original.occurred_at).toBeTruthy(); expect(reverseWalletAdjustment).not.toHaveBeenCalled()
   vi.mocked(fetchWalletAdjustment).mockResolvedValue({ ...row, external_status: 'failed' }); await form.trigger('submit'); await flushPromises(); expect(recordWalletPayout).toHaveBeenLastCalledWith(9, original); expect(reverseWalletAdjustment).not.toHaveBeenCalled()
   await wrapper.get('form[data-test="reverse"] input').setValue('工单确认未出款，恢复余额'); vi.mocked(reverseWalletAdjustment).mockResolvedValue({ ...row, external_status: 'failed', reversal_entry_id: 10 })
   await wrapper.get('form[data-test="reverse"]').trigger('submit'); await flushPromises(); expect(reverseWalletAdjustment).toHaveBeenCalledWith(9, '工单确认未出款，恢复余额'); expect(wrapper.find('form[data-test="reverse"]').exists()).toBe(false); wrapper.unmount()
 })
 it('discards old account responses and drafts without replaying writes', async () => {
   const wrapper = open(); await flushPromises(); await fillDebit(wrapper)
   let finish!: (value: any) => void; vi.mocked(fetchWalletAdjustment).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
   await wrapper.findAll('button').find(button => button.text() === '核对这笔出款')!.trigger('click'); await flushPromises(); state.app.user.id = 2; await flushPromises(); finish({ ...row, reason: 'old-private', results: [{ id: 1, note: 'old-private' }] }); await flushPromises()
   expect(wrapper.text()).not.toContain('old-private'); expect((wrapper.get('form[data-test="debit"] input').element as HTMLInputElement).value).toBe(''); expect(wrapper.find('form[data-test="payout"]').exists()).toBe(false); expect(debitWallet).not.toHaveBeenCalled(); expect(recordWalletPayout).not.toHaveBeenCalled(); wrapper.unmount()
 })
})
it('blocks a corrupt financial draft even if read-only records are refreshed', async () => {
 localStorage.setItem('zboard:wallet-adjustment:1:27:CNY', '{broken')
 const wrapper = open(); await flushPromises()
 await wrapper.findAll('button').find(button => button.text() === '核对余额')!.trigger('click'); await flushPromises()
 const form = await fillDebit(wrapper); expect(form.get('button[type="submit"]').attributes('disabled')).toBeDefined()
 await form.trigger('submit'); await flushPromises(); expect(debitWallet).not.toHaveBeenCalled(); wrapper.unmount()
})
