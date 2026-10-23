import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'
import SubscriptionTermination from './SubscriptionTermination.vue'
import { fetchSubscriptionValue, terminateSubscription, fetchAdministrativeSubscriptionValue, administrativelyTerminateSubscription } from '../api/subscriptionValue'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/subscriptionValue', () => ({ fetchSubscriptionValue: vi.fn(), terminateSubscription: vi.fn(), fetchAdministrativeSubscriptionValue: vi.fn(), administrativelyTerminateSubscription: vi.fn() }))
const quote = { subscription_id: 7, root_order_id: 3, currency: 'CNY', current_minor: '200', future_minor: '9000', total_minor: '9200', quote_fingerprint: 'a'.repeat(64) }
const result = { id: 9, subscription_id: 7, currency: 'CNY', amount_minor: '9200', status: 'canceled', revocation_status: 'pending_verification', created_at: '2026-10-19T00:00:00Z' }
function open() { return mount(SubscriptionTermination, { props: { subscriptionId: 7 } }) }
async function click(wrapper: ReturnType<typeof open>, label: string) { await wrapper.findAll('button').find(button => button.text() === label)!.trigger('click'); await flushPromises() }
beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); state.app = reactive({ user: { id: 1 } }); vi.mocked(fetchSubscriptionValue).mockResolvedValue(quote); vi.mocked(terminateSubscription).mockResolvedValue(result) })
it('requires explicit quote and confirmation; reload never replays the saved write', async () => {
 let wrapper = open(); await flushPromises(); expect(fetchSubscriptionValue).not.toHaveBeenCalled(); expect(terminateSubscription).not.toHaveBeenCalled()
 await click(wrapper, '核对退订残值'); expect(wrapper.text()).toContain('CNY 92.00'); expect(wrapper.text()).toContain('终止本份订阅的全部剩余权益'); expect(wrapper.text()).toContain('退的是剩余价值，不是原支付全款')
 vi.mocked(terminateSubscription).mockRejectedValueOnce(new Error('lost response')); await click(wrapper, '确认终止订阅并退款至余额')
 const original = vi.mocked(terminateSubscription).mock.calls[0]![1]; wrapper.unmount(); wrapper = open(); await flushPromises(); expect(terminateSubscription).toHaveBeenCalledTimes(1)
 await click(wrapper, '核对并重试原退订请求'); expect(terminateSubscription).toHaveBeenLastCalledWith(7, original); expect(wrapper.text()).toContain('节点撤权等待核验'); expect(wrapper.emitted('settled')).toHaveLength(1); wrapper.unmount()
})
it('needs a fresh explicit quote and confirmation after a conflict', async () => {
 const wrapper = open(); await click(wrapper, '核对退订残值'); vi.mocked(terminateSubscription).mockRejectedValueOnce({ response: { status: 409, data: { message: '用量已变化' } } }); await click(wrapper, '确认终止订阅并退款至余额')
 expect(wrapper.text()).toContain('用量已变化'); const old = vi.mocked(terminateSubscription).mock.calls[0]![1]
 vi.mocked(fetchSubscriptionValue).mockResolvedValue({ ...quote, quote_fingerprint: 'b'.repeat(64), total_minor: '9100' }); await click(wrapper, '重新核对残值'); expect(terminateSubscription).toHaveBeenCalledTimes(1)
 await click(wrapper, '确认终止订阅并退款至余额'); expect(vi.mocked(terminateSubscription).mock.calls[1]![1].idempotency_key).not.toBe(old.idempotency_key); wrapper.unmount()
})
it('isolates another account and discards late previous-account quotes', async () => {
 const wrapper = open(); let finish!: (value: any) => void; vi.mocked(fetchSubscriptionValue).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
 await wrapper.get('button').trigger('click'); state.app.user.id = 2; await flushPromises(); finish(quote); await flushPromises()
 expect(wrapper.text()).not.toContain('CNY 92.00'); expect(terminateSubscription).not.toHaveBeenCalled(); wrapper.unmount()
})
it('prevents writes when the persistent request is corrupt or cannot be saved', async () => {
 localStorage.setItem('zboard:unsubscribe:1:7', '{broken'); const wrapper = open(); expect(wrapper.get('button').attributes('disabled')).toBeDefined(); expect(terminateSubscription).not.toHaveBeenCalled(); wrapper.unmount()
 localStorage.clear(); const next = open(); await click(next, '核对退订残值'); const storage = localStorage; vi.stubGlobal('localStorage', { getItem: (key: string) => storage.getItem(key), setItem: () => { throw new Error('storage unavailable') } }); await click(next, '确认终止订阅并退款至余额'); expect(terminateSubscription).not.toHaveBeenCalled(); vi.unstubAllGlobals(); next.unmount()
})

it('administrator can explicitly terminate for zero without dispatching the owner write; reload never replays approval', async () => {
 state.app.user.isAdmin = true
 vi.mocked(fetchAdministrativeSubscriptionValue).mockResolvedValue({ ...quote, user_id: 27, max_refund_minor: '12000' })
 vi.mocked(administrativelyTerminateSubscription).mockRejectedValueOnce(new Error('response lost'))
 let wrapper = mount(SubscriptionTermination, { props: { subscriptionId: 7, administrative: true } })
 expect(fetchAdministrativeSubscriptionValue).not.toHaveBeenCalled()
 await click(wrapper, '核对退订残值')
 const inputs = wrapper.findAll('input'); expect(inputs[0]!.element.value).toBe('92.00')
 await inputs[0]!.setValue('0'); await inputs[1]!.setValue('管理员确认整份终止且不退款')
 await click(wrapper, '确认终止订阅（不退款）')
 expect(terminateSubscription).not.toHaveBeenCalled()
 const original = vi.mocked(administrativelyTerminateSubscription).mock.calls[0]![1]
 expect(original).toMatchObject({ amount_minor: 0, reason: '管理员确认整份终止且不退款' })
 wrapper.unmount()
 wrapper = mount(SubscriptionTermination, { props: { subscriptionId: 7, administrative: true } }); await flushPromises()
 expect(administrativelyTerminateSubscription).toHaveBeenCalledOnce()
 vi.mocked(administrativelyTerminateSubscription).mockResolvedValueOnce({ ...result, amount_minor: '0' })
 await click(wrapper, '核对并重试原退订请求')
 expect(administrativelyTerminateSubscription).toHaveBeenLastCalledWith(7, original)
 expect(wrapper.text()).toContain('节点撤权等待核验')
 wrapper.unmount()
})
it('administrator amount is bounded by verified payment, can exceed default residual, and requires an auditable reason', async () => {
 state.app.user.isAdmin = true
 vi.mocked(fetchAdministrativeSubscriptionValue).mockResolvedValue({ ...quote, user_id: 27, max_refund_minor: '12000' })
 vi.mocked(administrativelyTerminateSubscription).mockResolvedValue({ ...result, amount_minor: '9500' })
 const wrapper = mount(SubscriptionTermination, { props: { subscriptionId: 7, administrative: true } })
 await click(wrapper, '核对退订残值'); const inputs = wrapper.findAll('input')
 await inputs[1]!.setValue('工单核准整份退款')
 for (const invalid of ['121', '-1', '1.001']) {
  await inputs[0]!.setValue(invalid)
  expect(wrapper.findAll('button').find(button => button.text() === '确认终止订阅并退款至余额')!.attributes('disabled')).toBeDefined()
 }
 await inputs[0]!.setValue('95')
 await click(wrapper, '确认终止订阅并退款至余额')
 expect(administrativelyTerminateSubscription).toHaveBeenCalledWith(7, expect.objectContaining({ amount_minor: 9500 }))
 expect(terminateSubscription).not.toHaveBeenCalled()
 wrapper.unmount()
})
it('administrator approval is unavailable after current privilege is lost', async () => {
 state.app.user.isAdmin = false
 const wrapper = mount(SubscriptionTermination, { props: { subscriptionId: 7, administrative: true } })
 expect(wrapper.get('button').attributes('disabled')).toBeDefined()
 expect(fetchAdministrativeSubscriptionValue).not.toHaveBeenCalled()
 expect(administrativelyTerminateSubscription).not.toHaveBeenCalled()
 wrapper.unmount()
})

it('same-account reauthentication preserves an unsubmitted administrator approval', async () => {
 state.app.user.isAdmin = true
 vi.mocked(fetchAdministrativeSubscriptionValue).mockResolvedValue({ ...quote, user_id: 27, max_refund_minor: '12000' })
 const wrapper = mount(SubscriptionTermination, { props: { subscriptionId: 7, administrative: true } })
 await click(wrapper, '核对退订残值'); const inputs = wrapper.findAll('input')
 await inputs[0]!.setValue('95'); await inputs[1]!.setValue('等待管理员最终确认')
 state.app.user = { id: 1, isAdmin: true }; await flushPromises()
 expect(wrapper.findAll('input')[0]!.element.value).toBe('95')
 expect(wrapper.findAll('input')[1]!.element.value).toBe('等待管理员最终确认')
 expect(administrativelyTerminateSubscription).not.toHaveBeenCalled()
 wrapper.unmount()
})
