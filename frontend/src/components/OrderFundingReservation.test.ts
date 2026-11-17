import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'
import OrderFundingPayment from './OrderFundingPayment.vue'
import { configureOrderFunding, fetchOrderFunding, payOrderFromWallet } from '../api/orderFunding'
import { cancelOrder } from '../api/client'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/orderFunding', () => ({ configureOrderFunding: vi.fn(), fetchOrderFunding: vi.fn(), payOrderFromWallet: vi.fn() }))
vi.mock('../api/client', () => ({ cancelOrder: vi.fn() }))
const pending = { order_id: 27, configured: false, use_balance: false, currency: 'CNY', amount_minor: '10000', available_minor: '20000', wallet_minor: '0', cash_minor: '10000', state: 'unconfigured', expired: false, discount_reservation: { required: true as const, state: 'pending' as const, can_pay: false, can_cancel: true, can_reconcile: false, message: '优惠预留尚未确认，不能付款；金额保持不变。' } }
function open() { return mount(OrderFundingPayment, { props: { orderId: 27, currency: 'CNY' }, global: { stubs: { CheckoutPayment: true, RouterLink: { template: '<a><slot /></a>' } } } }) }
beforeEach(() => { vi.clearAllMocks(); state.app = reactive({ user: { id: 1 } }); vi.mocked(fetchOrderFunding).mockResolvedValue(pending); vi.mocked(cancelOrder).mockResolvedValue(undefined as any) })
it('blocks funding and channel/wallet payment while a required reservation is unknown', async () => {
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).toContain('优惠预留尚未确认')
  expect(wrapper.text()).not.toContain('先使用余额')
  expect(wrapper.find('checkout-payment-stub').exists()).toBe(false)
  expect(configureOrderFunding).not.toHaveBeenCalled(); expect(payOrderFromWallet).not.toHaveBeenCalled()
  await wrapper.findAll('button').find(button => button.text() === '核对资金分配')!.trigger('click'); await flushPromises()
  expect(configureOrderFunding).not.toHaveBeenCalled(); expect(payOrderFromWallet).not.toHaveBeenCalled()
  await wrapper.findAll('button').find(button => button.text() === '取消本次优惠订单')!.trigger('click'); await flushPromises()
  expect(cancelOrder).toHaveBeenCalledWith(27)
  wrapper.unmount()
})
it('a later confirmed read enables only explicit funding and does not replay an attempted payment', async () => {
  vi.mocked(fetchOrderFunding).mockResolvedValueOnce({ ...pending, configured: true, state: 'reserved', wallet_minor: '10000', cash_minor: '0' })
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).not.toContain('确认使用余额支付')
  expect(payOrderFromWallet).not.toHaveBeenCalled()
  vi.mocked(fetchOrderFunding).mockResolvedValue({ ...pending, discount_reservation: { ...pending.discount_reservation, state: 'confirmed', can_pay: true, message: '优惠已确认' } })
  await wrapper.findAll('button').find(button => button.text() === '核对资金分配')!.trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('先使用余额')
  expect(configureOrderFunding).not.toHaveBeenCalled(); expect(payOrderFromWallet).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('does not offer illegal cancellation for a failed discounted order and exposes only a read-only existing-payment entry', async () => {
  vi.mocked(fetchOrderFunding).mockResolvedValue({ ...pending, configured: true, state: 'released', discount_reservation: { ...pending.discount_reservation, state: 'closed', can_cancel: false, can_reconcile: true } })
  const wrapper = open(); await flushPromises()
  expect(wrapper.text()).not.toContain('取消本次优惠订单')
  expect(wrapper.text()).not.toContain('取消订单并释放预留')
  expect(wrapper.text()).not.toContain('先使用余额')
  const recovery = wrapper.findComponent({ name: 'CheckoutPayment' })
  expect(recovery.exists()).toBe(true)
  expect(recovery.props('reconcileOnly')).toBe(true)
  expect(recovery.props('reconcileOnLoad')).toBe(false)
  expect(recovery.props('openOnLoad')).toBe(false)
  expect(recovery.props('actorId')).toBe(1)
  expect(recovery.props('orderId')).toBe(27)
  expect(recovery.props('channelAmountMinor')).toBe('10000')
  expect(cancelOrder).not.toHaveBeenCalled(); expect(configureOrderFunding).not.toHaveBeenCalled(); expect(payOrderFromWallet).not.toHaveBeenCalled()
  const before = wrapper.emitted('changed')?.length || 0
  recovery.vm.$emit('reconciled', 27); await flushPromises()
  expect(wrapper.emitted('changed')).toHaveLength(before + 1)
  expect(fetchOrderFunding).toHaveBeenCalledTimes(2)
  wrapper.unmount()
})
it('clears the prior-account recovery entry and ignores an old funding read without replaying cancellation or payment', async () => {
  let finish!: (value: any) => void
  vi.mocked(fetchOrderFunding).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const wrapper = open(); await flushPromises()
  vi.mocked(fetchOrderFunding).mockRejectedValue(new Error('foreign order'))
  state.app.user.id = 2; await flushPromises()
  finish({ ...pending, discount_reservation: { ...pending.discount_reservation, can_reconcile: true } }); await flushPromises()
  expect(wrapper.find('checkout-payment-stub').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('取消本次优惠订单')
  expect(cancelOrder).not.toHaveBeenCalled(); expect(configureOrderFunding).not.toHaveBeenCalled(); expect(payOrderFromWallet).not.toHaveBeenCalled()
  wrapper.unmount()
})
