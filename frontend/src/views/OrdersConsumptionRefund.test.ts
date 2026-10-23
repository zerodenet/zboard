import { defineComponent } from 'vue'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, expect, it, vi } from 'vitest'
import Orders from './Orders.vue'
import AdminConsumptionRefund from '../components/AdminConsumptionRefund.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import { fetchAdminOrderDetail, fetchAdminOrderPaymentEvents, fetchOrdersPage, type AdminOrderDetail } from '../api/client'
import type { ConsumptionRefund } from '../api/consumptionRefunds'

vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchAdminOrderDetail: vi.fn(), fetchAdminOrderPaymentEvents: vi.fn(), fetchOrdersPage: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 1 }, isAdmin: true, systemTimeZone: 'UTC' }) }))
const order = { id: 10, user_id: 27, user_email: 'buyer@example.test', currency: 'CNY', status: 'paid', order_type: 'upgrade', paid_amount: 12000, payable_amount: 12000, refund_amount: 0, amount_cents: 12000, discount_amount: 0, fulfilled_at: '2026-09-01', subscription_ended_at: '2026-10-01', subscription_end_reason: 'expired', subscription_id: 91, target_subscription_id: 91, plan_id: 1, plan_sku_id: 1, trade_no: 'order-10', billing_unit: 'month', billing_value: 1, traffic_bytes: 100, failure_reason: '' } as AdminOrderDetail
const refund: ConsumptionRefund = { id: 4, order_id: 10, user_id: 27, currency: 'CNY', amount_minor: '900', ticket_id: 12, actor_id: 1, entry_id: 88, reason: '工单核准退款', created_at: '2026-10-20T00:00:00Z' }
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(fetchAdminOrderDetail).mockResolvedValue({ ...order })
  vi.mocked(fetchOrdersPage).mockResolvedValue({ items: [{ ...order }], total: 1 } as any)
  vi.mocked(fetchAdminOrderPaymentEvents).mockResolvedValue({ items: [], total: 0 } as any)
})
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/orders', component: Orders }] })
  await router.push('/admin/orders?order=10&user_id=27&status=paid&return_to=/admin/users')
  const wrapper = shallowMount(Orders, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: {
    DetailDrawer: defineComponent({ props: ['open'], template: '<aside v-if="open"><slot /></aside>' }),
    DataWorkbench: defineComponent({ template: '<div><slot name="filters" /><slot /><slot name="footer" /></div>' }),
    PageRefreshButton: true, UiButton: true, TimeBadge: true, FormField: true, UiInput: true, UiSelect: true,
  } } })
  await flushPromises()
  return { wrapper, router }
}
it('mounts the historical original-order refund panel and refreshes only current order reads after approval', async () => {
  const { wrapper, router } = await render()
  try {
    expect(wrapper.getComponent(AdminConsumptionRefund).props('order')).toMatchObject({ id: 10, subscription_id: 91, subscription_ended_at: '2026-10-01' })
    const originalRoute = router.currentRoute.value.fullPath
    wrapper.getComponent(AdminConsumptionRefund).vm.$emit('changed', refund)
    await flushPromises()
    expect(fetchAdminOrderDetail).toHaveBeenCalledTimes(2)
    expect(fetchAdminOrderDetail).toHaveBeenLastCalledWith(10, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(fetchOrdersPage).toHaveBeenCalledTimes(2)
    expect(router.currentRoute.value.fullPath).toBe(originalRoute)
    expect(wrapper.getComponent(TransientFeedback).props('success')).toBe('退款 #4 已退入用户 #27 的站内余额。')
  } finally { wrapper.unmount() }
})
it.each([{ status: 'pending' }, { order_type: 'topup' }])('does not attach a consumption refund action to a non-consumption or unpaid order: %j', async delta => {
  vi.mocked(fetchAdminOrderDetail).mockResolvedValue({ ...order, ...delta })
  const { wrapper } = await render()
  expect(wrapper.findComponent(AdminConsumptionRefund).exists()).toBe(false)
  wrapper.unmount()
})
