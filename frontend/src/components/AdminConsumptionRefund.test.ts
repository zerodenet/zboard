import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminConsumptionRefund from './AdminConsumptionRefund.vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { approveConsumptionRefund, fetchConsumptionRefund } from '../api/consumptionRefunds'
import { fetchAdministrativeWallet } from '../api/walletAdjustments'
import type { AdminOrderDetail } from '../api/client'
import type { ConsumptionRefund } from '../api/consumptionRefunds'

const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/consumptionRefunds', () => ({ approveConsumptionRefund: vi.fn(), fetchConsumptionRefund: vi.fn() }))
vi.mock('../api/walletAdjustments', () => ({ fetchAdministrativeWallet: vi.fn() }))
const order = { id: 10, user_id: 27, currency: 'CNY', status: 'paid', order_type: 'purchase', paid_amount: 12000, refund_amount: 0, fulfilled_at: '2026-09-01', subscription_ended_at: '2026-10-01', subscription_end_reason: 'expired', subscription_id: 9 } as AdminOrderDetail
const fact: ConsumptionRefund = { id: 4, order_id: 10, user_id: 27, currency: 'CNY', amount_minor: '900', ticket_id: 12, actor_id: 1, entry_id: 88, reason: '工单核准退款', created_at: '2026-10-20T00:00:00Z' }
const key = 'zboard:consumption-refund:1:10:27:CNY'
function render(value = order) {
  return mount(AdminConsumptionRefund, { props: { order: { ...value } }, global: { stubs: { ConfirmDialog: true, RouterLink: { props: ['to'], template: '<a :data-to="JSON.stringify(to)"><slot /></a>' } } } })
}
async function fill(wrapper: ReturnType<typeof render>, amount = '9.00') {
  const form = wrapper.get('form[data-test="consumption-refund"]')
  const inputs = form.findAll('input')
  await inputs[0]!.setValue(amount); await inputs[1]!.setValue('12'); await inputs[2]!.setValue('工单核准退款')
  return form
}
async function confirm(wrapper: ReturnType<typeof render>) {
  await wrapper.get('form').trigger('submit')
  wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
  await flushPromises()
}
beforeEach(() => {
  localStorage.clear(); vi.resetAllMocks()
  state.app = reactive({ user: { id: 1 }, isAdmin: true })
  vi.mocked(approveConsumptionRefund).mockResolvedValue({ ...fact })
  vi.mocked(fetchConsumptionRefund).mockResolvedValue({ ...fact })
  vi.mocked(fetchAdministrativeWallet).mockResolvedValue({ currency: 'CNY', balance_minor: '1900', reserved_minor: '100', available_minor: '1800', entries: [], has_more: false })
})

describe('administrative consumption refunds', () => {
  it('requires an explicit confirmation, records the exact request and exposes server fact, source, wallet and audit', async () => {
    const wrapper = render(); await flushPromises()
    expect(approveConsumptionRefund).not.toHaveBeenCalled()
    expect(fetchAdministrativeWallet).not.toHaveBeenCalled()
    const form = await fill(wrapper)
    await form.trigger('submit')
    expect(approveConsumptionRefund).not.toHaveBeenCalled()
    expect(wrapper.getComponent(ConfirmDialog).props('message')).toContain('CNY 9.00')
    expect(wrapper.getComponent(ConfirmDialog).props('message')).toContain('不修改当前新权益')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm'); await flushPromises()
    const request = vi.mocked(approveConsumptionRefund).mock.calls[0]![1]
    expect(request).toEqual({ amount_minor: 900, ticket_id: 12, reason: '工单核准退款', idempotency_key: expect.stringMatching(/^[a-f0-9]{32}$/) })
    expect(JSON.parse(localStorage.getItem(key)!).request).toEqual(request)
    expect(JSON.parse(localStorage.getItem(key)!).refund_id).toBe(4)
    expect(wrapper.get('[aria-label="已确认退款事实"]').text()).toContain('CNY 9.00 已退入站内余额')
    expect(wrapper.text()).toContain('来源订单 #10'); expect(wrapper.text()).toContain('余额流水 #88')
    expect(wrapper.text()).toContain('审批人 #1'); expect(wrapper.text()).toContain('可用 CNY 18.00')
    expect(wrapper.findAll('a').some(link => link.attributes('data-to')?.includes('order.consumption_refund'))).toBe(true)
    expect(wrapper.emitted('changed')).toEqual([[fact]])
    wrapper.unmount()
  })

  it('preserves an unknown request through reload and only explicitly retries the same payload and identity', async () => {
    vi.mocked(approveConsumptionRefund).mockRejectedValueOnce(new Error('response lost'))
    const wrapper = render(); await fill(wrapper); await confirm(wrapper)
    const request = JSON.parse(JSON.stringify(vi.mocked(approveConsumptionRefund).mock.calls[0]![1]))
    expect(wrapper.get('form').findAll('input').every(input => input.attributes('disabled') !== undefined)).toBe(true)
    wrapper.unmount()
    const restored = render(); await flushPromises()
    expect(approveConsumptionRefund).toHaveBeenCalledTimes(1)
    expect(fetchConsumptionRefund).not.toHaveBeenCalled()
    expect(restored.text()).toContain(request.idempotency_key)
    expect(restored.findAll('button').some(button => button.text() === '修改被拒绝的申请')).toBe(false)
    await confirm(restored)
    expect(approveConsumptionRefund).toHaveBeenCalledTimes(2)
    expect(approveConsumptionRefund).toHaveBeenLastCalledWith(10, request)
    restored.unmount()
  })

  it('only permits a new corrected intent after an explicit validation refusal and another confirmation', async () => {
    vi.mocked(approveConsumptionRefund).mockRejectedValueOnce({ response: { status: 400, data: { message: '工单不存在或不属于原订单账户。' } } })
    const wrapper = render(); await fill(wrapper)
    await wrapper.get('form').findAll('input')[1]!.setValue('999')
    await confirm(wrapper)
    const first = vi.mocked(approveConsumptionRefund).mock.calls[0]![1].idempotency_key
    expect(wrapper.text()).toContain('服务器已明确拒绝这份申请')
    expect(wrapper.find('[aria-label="已确认退款事实"]').exists()).toBe(false)
    await wrapper.findAll('button').find(button => button.text() === '修改被拒绝的申请')!.trigger('click')
    expect(approveConsumptionRefund).toHaveBeenCalledOnce()
    await fill(wrapper)
    await confirm(wrapper)
    expect(approveConsumptionRefund).toHaveBeenCalledTimes(2)
    expect(vi.mocked(approveConsumptionRefund).mock.calls[1]![1]).toMatchObject({ ticket_id: 12 })
    expect(vi.mocked(approveConsumptionRefund).mock.calls[1]![1].idempotency_key).not.toBe(first)
    wrapper.unmount()
  })

  it.each([401, 403, 409, 500])('does not clear a potentially committed request merely because a later response has HTTP %s', async status => {
    vi.mocked(approveConsumptionRefund).mockRejectedValueOnce({ response: { status, data: {} } })
    const wrapper = render(); await fill(wrapper); await confirm(wrapper)
    expect(wrapper.findAll('button').some(button => button.text() === '修改被拒绝的申请')).toBe(false)
    expect(wrapper.get('form').findAll('input').every(input => input.attributes('disabled') !== undefined)).toBe(true)
    expect(wrapper.find('[aria-label="已确认退款事实"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('restores a known fact with GET and never replays its approval', async () => {
    const wrapper = render(); await fill(wrapper); await confirm(wrapper); wrapper.unmount()
    const restored = render(); await flushPromises()
    expect(fetchConsumptionRefund).toHaveBeenCalledWith(4, expect.any(AbortSignal))
    expect(approveConsumptionRefund).toHaveBeenCalledOnce()
    expect(restored.get('[aria-label="已确认退款事实"]').text()).toContain('退款 #4')
    restored.unmount()
  })

  it('does not accept a different refund id as verification of the known record', async () => {
    const wrapper = render(); await fill(wrapper); await confirm(wrapper); wrapper.unmount()
    vi.mocked(fetchAdministrativeWallet).mockClear()
    vi.mocked(fetchConsumptionRefund).mockResolvedValueOnce({ ...fact, id: 99 })
    const restored = render(); await flushPromises()
    expect(fetchConsumptionRefund).toHaveBeenCalledWith(4, expect.any(AbortSignal))
    expect(restored.find('[aria-label="已确认退款事实"]').exists()).toBe(false)
    expect(restored.text()).toContain('退款记录暂时无法核对')
    expect(restored.text()).not.toContain('退款 #99')
    expect(fetchAdministrativeWallet).not.toHaveBeenCalled()
    expect(approveConsumptionRefund).toHaveBeenCalledOnce()
    restored.unmount()
  })

  it('retains a confirmed refund while a separate wallet read fails and recovers using reads only', async () => {
    vi.mocked(fetchAdministrativeWallet).mockRejectedValueOnce(new Error('wallet offline'))
    const wrapper = render(); await fill(wrapper); await confirm(wrapper)
    expect(wrapper.get('[aria-label="已确认退款事实"]').text()).toContain('已退入站内余额')
    expect(wrapper.text()).toContain('当前余额未知')
    expect(wrapper.text()).not.toContain('余额 CNY 0.00')
    await wrapper.findAll('button').find(button => button.text() === '核对退款记录与余额')!.trigger('click'); await flushPromises()
    expect(approveConsumptionRefund).toHaveBeenCalledOnce()
    expect(fetchConsumptionRefund).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('余额 CNY 19.00')
    wrapper.unmount()
  })

  it.each([
    { subscription_ended_at: null }, { subscription_end_reason: undefined }, { fulfilled_at: null }, { order_type: 'topup' }, { status: 'pending' },
  ])('does not offer an executable refund without the supported historical consumption boundary: %j', delta => {
    const wrapper = render({ ...order, ...delta } as AdminOrderDetail)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.text()).toContain('当前订单不能执行消费退款')
    expect(wrapper.text()).toContain('整份退订，按剩余价值退款并终止全部剩余权益')
    expect(approveConsumptionRefund).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not dispatch invalid precision or a request whose local identity cannot be saved', async () => {
    const wrapper = render(); const form = await fill(wrapper, '9.001')
    await form.trigger('submit')
    expect(wrapper.getComponent(ConfirmDialog).props('open')).toBe(false)
    await fill(wrapper)
    const write = vi.spyOn(localStorage, 'setItem').mockImplementation(() => { throw new Error('full') })
    try { await confirm(wrapper) } finally { write.mockRestore() }
    expect(approveConsumptionRefund).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('本次未发起退款')
    wrapper.unmount()
  })

  it('fails closed on a corrupt financial draft instead of manufacturing a new request', async () => {
    localStorage.setItem(key, '{broken')
    const wrapper = render(); const form = await fill(wrapper)
    expect(form.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await confirm(wrapper)
    expect(approveConsumptionRefund).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('本地退款申请记录无法可靠读取')
    wrapper.unmount()
  })

  it('does not expose a previous administrator response or draft to another actor', async () => {
    let finish!: (value: ConsumptionRefund) => void
    vi.mocked(approveConsumptionRefund).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = render(); await fill(wrapper); await confirm(wrapper)
    const original = JSON.parse(localStorage.getItem(key)!)
    state.app.user.id = 2; await flushPromises()
    finish({ ...fact }); await flushPromises()
    expect(wrapper.find('[aria-label="已确认退款事实"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(original.request.idempotency_key)
    expect((wrapper.get('form input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(localStorage.getItem('zboard:consumption-refund:2:10:27:CNY')).toBeNull()
    wrapper.unmount()
  })

  it('isolates another order and ignores mismatched server facts', async () => {
    vi.mocked(approveConsumptionRefund).mockResolvedValueOnce({ ...fact, order_id: 99 })
    const wrapper = render(); await fill(wrapper); await confirm(wrapper)
    expect(wrapper.find('[aria-label="已确认退款事实"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toBeUndefined()
    await wrapper.setProps({ order: { ...order, id: 11 } }); await flushPromises()
    expect((wrapper.get('form input').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).not.toContain('退款请求标识：')
    expect(approveConsumptionRefund).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('allows a separate explicitly confirmed refund after a verified success without calculating the remaining budget', async () => {
    const wrapper = render(); await fill(wrapper); await confirm(wrapper)
    const first = vi.mocked(approveConsumptionRefund).mock.calls[0]![1].idempotency_key
    await wrapper.findAll('button').find(button => button.text() === '开始另一笔退款申请')!.trigger('click')
    expect(approveConsumptionRefund).toHaveBeenCalledOnce()
    await fill(wrapper); await confirm(wrapper)
    expect(approveConsumptionRefund).toHaveBeenCalledTimes(2)
    expect(vi.mocked(approveConsumptionRefund).mock.calls[1]![1].idempotency_key).not.toBe(first)
    expect(wrapper.text()).toContain('最终可退预算由服务器核对')
    wrapper.unmount()
  })
})
