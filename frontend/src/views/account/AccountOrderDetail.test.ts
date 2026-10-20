import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountOrder } from '../../api/client'
import AccountOrderDetail from './AccountOrderDetail.vue'

vi.mock('../../api/client', () => ({ fetchAccountOrder: vi.fn() }))
const order = (id = 27, status = 'pending') => ({ id, status, plan_name: 'Pass Iron', sku_name: '月付', payable_amount: 1500, amount_cents: 1500, currency: 'CNY', trade_no: `trade-${id}`, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z' } as any)
async function open(path = '/account/orders/27') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/account/orders/:id', component: AccountOrderDetail },
    { path: '/account/orders', component: { template: '<div />' } },
    { path: '/account/tickets', component: { template: '<div />' } },
  ] })
  await router.push(path); await router.isReady()
  const wrapper = mount(AccountOrderDetail, { global: { plugins: [router], stubs: { CheckoutPayment: true, OrderFundingPayment: true, PaymentReceipts: true } } })
  await flushPromises()
  return { wrapper, router }
}
describe('account order detail payment ownership', () => {
  beforeEach(() => { vi.clearAllMocks(); vi.mocked(fetchAccountOrder).mockResolvedValue(order()) })
  it('loads only the requested order and scopes payment to it without automatically opening confirmation', async () => {
    const { wrapper } = await open()
    expect(fetchAccountOrder).toHaveBeenCalledWith(27, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('订单 #27')
    expect(wrapper.text()).toContain('Pass Iron')
    expect(wrapper.text()).toContain('¥15.00')
    const payment = wrapper.get('order-funding-payment-stub')
    expect(payment.attributes('orderid')).toBe('27')
    expect(payment.attributes('currency')).toBe('CNY')
    expect(wrapper.get('a').attributes('href')).toBe('/account/orders')
    wrapper.unmount()
  })
  it.each(['paid', 'canceled'])('does not load payment plugins for a %s order', async status => {
    vi.mocked(fetchAccountOrder).mockResolvedValue(order(27, status))
    const { wrapper } = await open('/account/orders/27?payment_plugin=example.plugin')
    expect(wrapper.find('order-funding-payment-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain(status === 'paid' ? '本站已确认' : '不可继续支付')
    wrapper.unmount()
  })
  it('retains generic return hints only for the current order and removes payment after host settlement', async () => {
    const { wrapper } = await open('/account/orders/27?payment_plugin=example.plugin')
    const payment = wrapper.getComponent({ name: 'OrderFundingPayment' })
    expect(payment.props()).toMatchObject({ orderId: 27, preferredPlugin: 'example.plugin', currency: 'CNY' })
    vi.mocked(fetchAccountOrder).mockResolvedValue(order(27, 'paid'))
    payment.vm.$emit('changed'); await flushPromises()
    expect(wrapper.find('order-funding-payment-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('已支付')
    wrapper.unmount()
  })
  it('does not mount payment when the order is inaccessible and allows a read retry', async () => {
    vi.mocked(fetchAccountOrder).mockRejectedValueOnce(new Error('not found'))
    const { wrapper } = await open()
    expect(wrapper.text()).toContain('订单不存在')
    expect(wrapper.find('order-funding-payment-stub').exists()).toBe(false)
    await wrapper.findAll('button').find(button => button.text() === '刷新订单')!.trigger('click'); await flushPromises()
    expect(wrapper.get('order-funding-payment-stub').attributes('orderid')).toBe('27')
    wrapper.unmount()
  })
  it('discards a late response when navigating to another order', async () => {
    let resolve!: (value: any) => void
    vi.mocked(fetchAccountOrder).mockReturnValueOnce(new Promise(done => { resolve = done }))
    const { wrapper, router } = await open()
    vi.mocked(fetchAccountOrder).mockResolvedValue(order(28))
    await router.push('/account/orders/28'); await flushPromises()
    resolve(order(27)); await flushPromises()
    expect(wrapper.text()).toContain('订单 #28')
    expect(wrapper.text()).not.toContain('trade-27')
    expect(wrapper.get('order-funding-payment-stub').attributes('orderid')).toBe('28')
    wrapper.unmount()
  })
  it('rejects invalid order identifiers without invoking the API or loading plugins', async () => {
    const { wrapper } = await open('/account/orders/invalid')
    expect(fetchAccountOrder).not.toHaveBeenCalled()
    expect(wrapper.find('order-funding-payment-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('订单不存在')
    wrapper.unmount()
  })
  it('links only the loaded account order to a new unsubmitted ticket context', async () => {
    const { wrapper, router } = await open()
    const support = wrapper.findAll('a').find(link => link.text() === '就此订单提交工单')!
    expect(support.attributes('href')).toBe('/account/tickets?new=1&order_id=27')
    expect(router.currentRoute.value.path).toBe('/account/orders/27')
    expect(support.attributes('href')).not.toContain('user_id')
    wrapper.unmount()
    vi.mocked(fetchAccountOrder).mockRejectedValue(new Error('inaccessible'))
    const unavailable = await open()
    expect(unavailable.wrapper.text()).not.toContain('就此订单提交工单')
    unavailable.wrapper.unmount()
  })
})
