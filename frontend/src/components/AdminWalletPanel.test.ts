import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminWalletPanel from './AdminWalletPanel.vue'
import { creditWallet, debitWallet, fetchAdministrativeWallet, fetchWalletAdjustments, fetchWalletAdjustment, recordWalletPayout, reverseWalletAdjustment } from '../api/walletAdjustments'
import UiSelect from './UiSelect.vue'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/walletAdjustments', () => ({ creditWallet: vi.fn(), debitWallet: vi.fn(), fetchAdministrativeWallet: vi.fn(), fetchWalletAdjustments: vi.fn(), fetchWalletAdjustment: vi.fn(), recordWalletPayout: vi.fn(), reverseWalletAdjustment: vi.fn() }))
const row = { id: 9, user_id: 27, currency: 'CNY', amount_minor: '900', reason: '工单特殊出款', ticket_id: 12, actor_id: 1, entry_id: 6, external_status: 'pending', created_at: '2026-10-19T00:00:00Z' }
function open() { return mount(AdminWalletPanel, { props: { userId: 27 }, global: { stubs: { ModalDialog: { props: ['open'], template: '<section v-if="open"><slot/><slot name="footer" :request-close="() => {}"/></section>' } } } }) }
async function expandPayout(wrapper: ReturnType<typeof open>) { if (wrapper.get('[data-test="payout-toggle"]').attributes('aria-expanded') !== 'true') { await wrapper.get('[data-test="payout-toggle"]').trigger('click'); await flushPromises() } }
async function fillDebit(wrapper: ReturnType<typeof open>) { await expandPayout(wrapper); const form = wrapper.get('form[data-test="debit"]'); await form.findAll('input')[0]!.setValue('9.00'); await form.findAll('input')[1]!.setValue('12'); await form.findAll('input')[2]!.setValue('工单特殊出款'); return form }
beforeEach(() => {
 vi.clearAllMocks(); localStorage.clear(); state.app = reactive({ user: { id: 1 } })
 vi.mocked(fetchAdministrativeWallet).mockResolvedValue({ currency: 'CNY', balance_minor: '1000', reserved_minor: '100', available_minor: '900', entries: [], has_more: false })
 vi.mocked(fetchWalletAdjustments).mockResolvedValue({ items: [row], has_more: false }); vi.mocked(fetchWalletAdjustment).mockResolvedValue(row)
 vi.mocked(creditWallet).mockResolvedValue({ id: 20, kind: 'admin_credit', balance_delta: '500', reserved_delta: '0', balance_after: '1500', reserved_after: '100', reason: '管理员增加余额：活动赠送', created_at: '2026-11-04T00:00:00Z' });
 vi.mocked(debitWallet).mockResolvedValue(row); vi.mocked(recordWalletPayout).mockResolvedValue(row)
})
describe('controlled wallet adjustment', () => {
 it('requires explicit debit and preserves the original ticketed request through a lost response and reload', async () => {
   const wrapper = open(); await flushPromises(); expect(wrapper.get('[aria-label="CNY 余额"]').text()).toContain('已预留1.00'); expect(debitWallet).not.toHaveBeenCalled()
   vi.mocked(debitWallet).mockRejectedValueOnce(new Error('response lost')); const form = await fillDebit(wrapper); await form.trigger('submit'); await flushPromises()
   const original = vi.mocked(debitWallet).mock.calls[0]![0]; expect(original).toEqual({ user_id: 27, currency: 'CNY', amount_minor: 900, ticket_id: 12, reason: '工单特殊出款', idempotency_key: expect.stringMatching(/^[a-f0-9]{32}$/) })
   expect(form.findAll('input').every(input => input.attributes('disabled') !== undefined)).toBe(true)
   await wrapper.get('button[aria-label="刷新余额"]').trigger('click'); await flushPromises(); expect(debitWallet).toHaveBeenCalledTimes(1)
   wrapper.unmount(); const restored = open(); await flushPromises(); expect(debitWallet).toHaveBeenCalledTimes(1)
   await restored.get('form[data-test="debit"]').trigger('submit'); await flushPromises(); expect(debitWallet).toHaveBeenLastCalledWith(original); expect(debitWallet).toHaveBeenCalledTimes(2); restored.unmount()
 })
 it('records manual payout results separately and preserves timestamp and identity on explicit retry', async () => {
   const wrapper = open(); await flushPromises(); await expandPayout(wrapper); await wrapper.findAll('button').find(button => button.text() === '核对这笔出款')!.trigger('click'); await flushPromises()
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
   expect(wrapper.text()).not.toContain('old-private'); await expandPayout(wrapper); expect((wrapper.get('form[data-test="debit"] input').element as HTMLInputElement).value).toBe(''); expect(wrapper.find('form[data-test="payout"]').exists()).toBe(false); expect(debitWallet).not.toHaveBeenCalled(); expect(recordWalletPayout).not.toHaveBeenCalled(); wrapper.unmount()
 })
})
it('blocks a corrupt financial draft even if read-only records are refreshed', async () => {
 localStorage.setItem('zboard:wallet-adjustment:1:27:CNY', '{broken')
 const wrapper = open(); await flushPromises()
 await wrapper.get('button[aria-label="刷新余额"]').trigger('click'); await flushPromises()
 const form = await fillDebit(wrapper); expect(form.get('button[type="submit"]').attributes('disabled')).toBeDefined()
 await form.trigger('submit'); await flushPromises(); expect(debitWallet).not.toHaveBeenCalled(); wrapper.unmount()
})

async function openCredit(wrapper: ReturnType<typeof open>) {
 await wrapper.get('[data-test="credit-open"]').trigger('click'); await flushPromises()
 const form = wrapper.get('form[data-test="credit"]')
 await form.get('#admin-wallet-credit-amount').setValue('5.00')
 await form.get('#admin-wallet-credit-reason').setValue('活动赠送')
 return form
}
describe('administrator wallet credits', () => {
 it('adds balance without a ticket and refreshes the ledger and parent list', async () => {
  const wrapper=open(); await flushPromises(); expect(creditWallet).not.toHaveBeenCalled()
  const form=await openCredit(wrapper); expect(form.findAll('input')).toHaveLength(2)
  await form.trigger('submit'); await flushPromises()
  expect(creditWallet).toHaveBeenCalledWith({user_id:27,currency:'CNY',amount_minor:500,reason:'活动赠送',idempotency_key:expect.stringMatching(/^[a-f0-9]{32}$/)})
  expect(wrapper.emitted('changed')).toHaveLength(1); expect(fetchAdministrativeWallet).toHaveBeenCalledTimes(2); wrapper.unmount()
 })
 it('rejects extra fractional precision at the amount field', async () => {
  const wrapper=open();await flushPromises();const form=await openCredit(wrapper)
  await form.get('#admin-wallet-credit-amount').setValue('5.001');await form.trigger('submit');await flushPromises()
  expect(creditWallet).not.toHaveBeenCalled();expect(form.get('#admin-wallet-credit-amount').attributes('aria-invalid')).toBe('true');wrapper.unmount()
 })
 it('keeps the same credit intent after a lost response, refresh and remount', async () => {
  const wrapper=open();await flushPromises();let form=await openCredit(wrapper)
  vi.mocked(creditWallet).mockRejectedValueOnce(new Error('response lost'));await form.trigger('submit');await flushPromises()
  const original=vi.mocked(creditWallet).mock.calls[0]![0];expect(form.findAll('input').every(input=>input.attributes('disabled')!==undefined)).toBe(true)
  await wrapper.get('button[aria-label="刷新余额"]').trigger('click');await flushPromises();expect(creditWallet).toHaveBeenCalledTimes(1);wrapper.unmount()
  const restored=open();await flushPromises();await restored.get('[data-test="credit-open"]').trigger('click');await flushPromises()
  form=restored.get('form[data-test="credit"]');await form.trigger('submit');await flushPromises()
  expect(creditWallet).toHaveBeenLastCalledWith(original);expect(creditWallet).toHaveBeenCalledTimes(2);restored.unmount()
 })
 it('does not expose old credit results after switching the target or identity', async () => {
  const wrapper=open();await flushPromises();const form=await openCredit(wrapper)
  let finish!:(value:any)=>void;vi.mocked(creditWallet).mockReturnValueOnce(new Promise(resolve=>{finish=resolve}))
  await form.trigger('submit');await wrapper.setProps({userId:28});await flushPromises()
  finish({id:20,kind:'admin_credit',balance_delta:'500',reserved_delta:'0'});await flushPromises()
  expect(wrapper.emitted('changed')).toBeUndefined();expect(wrapper.find('form[data-test="credit"]').exists()).toBe(false)
  expect(JSON.parse(localStorage.getItem('zboard:wallet-adjustment:1:27:CNY:credit')!).result).toBe(20);wrapper.unmount()
 })
 it('blocks corrupt credit intents without replaying or erasing them', async () => {
  localStorage.setItem('zboard:wallet-adjustment:1:27:CNY:credit','{broken')
  const wrapper=open();await flushPromises();const form=await openCredit(wrapper);await form.trigger('submit');await flushPromises()
  expect(creditWallet).not.toHaveBeenCalled();expect(form.text()).toContain('增加余额记录无法读取');expect(localStorage.getItem('zboard:wallet-adjustment:1:27:CNY:credit')).toBe('{broken');wrapper.unmount()
 })
})

it('uses the platform default for an empty wallet and honors an explicitly selected historical wallet', async () => {
 state.app.publicConfigs = [{ config_key: 'default_currency', value: 'USD' }]
 const fresh = open(); await flushPromises()
 expect(fetchAdministrativeWallet).toHaveBeenCalledWith(27, 'USD', 0, expect.any(AbortSignal))
 fresh.unmount(); vi.mocked(fetchAdministrativeWallet).mockClear()
 const existing = mount(AdminWalletPanel, { props: { userId: 27, initialCurrency: 'CNY' } }); await flushPromises()
 expect(fetchAdministrativeWallet).toHaveBeenCalledWith(27, 'CNY', 0, expect.any(AbortSignal))
 existing.unmount()
})
