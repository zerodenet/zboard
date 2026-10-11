import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { fetchAccountOrdersPage } from '../../api/client'
import AccountOrders from './AccountOrders.vue'

vi.mock('../../api/client', () => ({ cancelOrder: vi.fn(), fetchAccountOrdersPage: vi.fn() }))

describe('account order terminal subscription history', () => {
  it('replaces the return order when a different payment is selected and clears the old plugin hint', async () => {
    vi.mocked(fetchAccountOrdersPage).mockResolvedValue({
      items: [{ id: 1, status: 'pending', created_at: '', amount_cents: 100, currency: 'CNY' }, { id: 2, status: 'pending', created_at: '', amount_cents: 100, currency: 'CNY' }],
      total: 2, offset: 0, limit: 25,
      page: { offset: 0, limit: 25, total: 2, next_cursor: null, previous_cursor: null }, aggregates: {}, facets: {},
    } as any)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/account/orders', component: AccountOrders }] })
    await router.push('/account/orders?payment_order=1&payment_plugin=old.plugin'); await router.isReady()
    const wrapper = mount(AccountOrders, { global: { plugins: [router], stubs: { CheckoutPayment: true } } }); await flushPromises()
    await wrapper.findAll('button').filter(button => button.text() === '支付')[1].trigger('click'); await flushPromises()
    expect(router.currentRoute.value.query.payment_order).toBe('2')
    expect(router.currentRoute.value.query.payment_plugin).toBeUndefined()
    expect(wrapper.get('checkout-payment-stub').attributes('orderid')).toBe('2')
    expect(wrapper.get('checkout-payment-stub').attributes('preferredplugin')).toBe('')
    wrapper.unmount()
  })
  it('shows the final service state after its instance has been cleaned', async () => {
    vi.mocked(fetchAccountOrdersPage).mockResolvedValue({
      items: [{
        id: 1, user_id: 1, subscription_id: 14, plan_id: 1, plan_sku_id: 1,
        trade_no: 'history-order', order_type: 'new', amount_cents: 500,
        currency: 'CNY', status: 'paid', plan_name: 'Pass Trial', sku_name: '一次性',
        created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
        subscription_ended_at: '2026-09-27T00:00:00Z', subscription_end_reason: 'exhausted',
        subscription_final_flow_total: 1024, subscription_final_flow_used: 1024,
      }],
      total: 1, offset: 0, limit: 25,
      page: { offset: 0, limit: 25, total: 1, next_cursor: null, previous_cursor: null },
      aggregates: {}, facets: {},
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/account/orders', component: AccountOrders },
        { path: '/account/plans', component: { template: '<div />' } },
      ],
    })
    await router.push('/account/orders')
    await router.isReady()
    const wrapper = mount(AccountOrders, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Pass Trial')
    expect(wrapper.text()).toContain('原订阅已用完')
    expect(wrapper.text()).toContain('最终用量')
    expect(wrapper.text()).toContain('已支付')
    expect(wrapper.text()).toContain('¥5.00')
    wrapper.unmount()
  })
})
