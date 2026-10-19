import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OrderFundingPayment from './OrderFundingPayment.vue'
import { configureOrderFunding, fetchOrderFunding, payOrderFromWallet } from '../api/orderFunding'
import { cancelOrder } from '../api/client'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/orderFunding', () => ({ configureOrderFunding: vi.fn(), fetchOrderFunding: vi.fn(), payOrderFromWallet: vi.fn() }))
vi.mock('../api/client', () => ({ cancelOrder: vi.fn() }))
const initial = { order_id: 27, configured: false, use_balance: false, currency: 'CNY', amount_minor: '5000', available_minor: '3000', wallet_minor: '0', cash_minor: '5000', state: 'unconfigured', expired: false }
const mixed = { ...initial, configured: true, use_balance: true, available_minor: '0', wallet_minor: '3000', cash_minor: '2000', state: 'reserved', expires_at: '2026-10-19T19:00:00Z' }
function open() { return mount(OrderFundingPayment, { props: { orderId: 27, currency: 'CNY' }, global: { stubs: { CheckoutPayment: true, RouterLink: { template: '<a><slot /></a>' } } } }) }
beforeEach(() => {
  vi.clearAllMocks(); state.app = reactive({ user: { id: 1 } })
  vi.mocked(fetchOrderFunding).mockResolvedValue(initial)
  vi.mocked(configureOrderFunding).mockResolvedValue(mixed)
  vi.mocked(payOrderFromWallet).mockResolvedValue({ order_id: 27, status: 'paid', fulfilled: true })
})
describe('order funding decisions', () => {
  it('waits for an explicit choice and loads a channel only with the frozen cash supplement', async () => {
    const wrapper = open(); await flushPromises()
    expect(wrapper.text()).toContain('CNY 30.00')
    expect(wrapper.find('checkout-payment-stub').exists()).toBe(false)
    expect(configureOrderFunding).not.toHaveBeenCalled()
    expect(payOrderFromWallet).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(button => button.text().startsWith('先使用余额'))!.trigger('click'); await flushPromises()
    expect(configureOrderFunding).toHaveBeenCalledWith(27, true)
    expect(wrapper.text()).toContain('订单总额 CNY 50.00')
    expect(wrapper.get('checkout-payment-stub').attributes('channelamountminor')).toBe('2000')
    expect(payOrderFromWallet).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps the original funding choice after an uncertain response without changing or replaying writes', async () => {
    vi.mocked(configureOrderFunding).mockRejectedValueOnce(new Error('lost response'))
    const wrapper = open(); await flushPromises()
    await wrapper.findAll('button').find(button => button.text().startsWith('先使用余额'))!.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('恢复本次资金分配')
    expect(wrapper.text()).not.toContain('全部通过支付渠道')
    await wrapper.findAll('button').find(button => button.text() === '核对资金分配')!.trigger('click'); await flushPromises()
    expect(configureOrderFunding).toHaveBeenCalledTimes(1)
    await wrapper.findAll('button').find(button => button.text() === '恢复本次资金分配')!.trigger('click'); await flushPromises()
    expect(configureOrderFunding).toHaveBeenNthCalledWith(2, 27, true)
    wrapper.unmount()
  })
  it('requires a separate wallet confirmation and never automatically retries after an uncertain payment', async () => {
    vi.mocked(fetchOrderFunding).mockResolvedValue({ ...mixed, wallet_minor: '5000', cash_minor: '0' })
    const wrapper = open(); await flushPromises()
    expect(wrapper.find('checkout-payment-stub').exists()).toBe(false)
    expect(payOrderFromWallet).not.toHaveBeenCalled()
    vi.mocked(payOrderFromWallet).mockRejectedValueOnce(new Error('payment response lost'))
    await wrapper.findAll('button').find(button => button.text().startsWith('确认使用余额支付'))!.trigger('click'); await flushPromises()
    expect(payOrderFromWallet).toHaveBeenCalledWith(27)
    await wrapper.findAll('button').find(button => button.text() === '核对资金分配')!.trigger('click'); await flushPromises()
    expect(payOrderFromWallet).toHaveBeenCalledTimes(1)
    vi.mocked(fetchOrderFunding).mockRejectedValue(new Error('foreign order'))
    state.app.user.id = 2; await flushPromises()
    expect(wrapper.text()).not.toContain('订单总额 CNY 50.00')
    expect(payOrderFromWallet).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('does not offer collection after a release and allows explicit cancellation of an expired hold', async () => {
    vi.mocked(fetchOrderFunding).mockResolvedValue({ ...mixed, state: 'released' })
    const wrapper = open(); await flushPromises()
    expect(wrapper.text()).toContain('资金分配已失效')
    expect(wrapper.find('checkout-payment-stub').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    vi.mocked(fetchOrderFunding).mockResolvedValue({ ...mixed, expired: true })
    await wrapper.findAll('button').find(button => button.text() === '核对资金分配')!.trigger('click'); await flushPromises()
    vi.mocked(cancelOrder).mockResolvedValue(undefined as any)
    await wrapper.findAll('button').find(button => button.text() === '取消订单并释放预留')!.trigger('click'); await flushPromises()
    expect(cancelOrder).toHaveBeenCalledWith(27)
    expect(payOrderFromWallet).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
