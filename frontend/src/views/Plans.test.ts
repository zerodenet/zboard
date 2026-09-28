import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Plans from './Plans.vue'
import SKUCycleFields from '../components/SKUCycleFields.vue'
import TableText from '../components/TableText.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import FormField from '../components/FormField.vue'
import { fetchPlanDetail, fetchPlanSKU, fetchPlanSKUs, fetchPlansPage, updatePlan, updatePlanSKU, deletePlan, deletePlanSKU } from '../api/client'
import { confirmAction } from '../utils/feedback'

vi.mock('../api/client', () => ({ createPlan: vi.fn(), createPlanSKU: vi.fn(), fetchPlanDetail: vi.fn(), fetchPlanSKU: vi.fn(), fetchPlanSKUs: vi.fn(), fetchPlansPage: vi.fn(), updatePlan: vi.fn(), updatePlanSKU: vi.fn(), deletePlan: vi.fn(), deletePlanSKU: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ isAdmin: true }) }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn() }))
const plan = { id: 1, name: 'Starter', slug: 'starter', summary: '', description: '', node_group_id: 1, traffic_bytes: 1024 ** 3, speed_limit_mbps: 0, max_active_subscriptions: 0, is_renewable: true, device_limit: 1, family_limit: 0, reset_policy: 2, traffic_calc_mode: 0, is_active: true, sort_order: 0, revision: 1, sku_count: 1, active_sku_count: 1 }
const sku = { id: 1, plan_id: 1, code: 'starter-month', name: 'Month', sku_type: 'new', billing_mode: 'periodic', entitlement_mode: 'plan', renewal_effect: 'none', allowed_operations: ['purchase'], billing_unit: 'month', billing_value: 1, currency: 'CNY', price_cents: 1000, is_active: true, sort_order: 0, grant_traffic_bytes: 0 }
const modal = defineComponent({ props: ['open'], template: '<div v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></div>' })
const button = defineComponent({ template: '<button><slot /></button>' })
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(confirmAction).mockResolvedValue(true)
  vi.mocked(fetchPlanDetail).mockResolvedValue(structuredClone(plan) as any)
  vi.mocked(fetchPlanSKU).mockResolvedValue(structuredClone(sku) as any)
  vi.mocked(fetchPlanSKUs).mockResolvedValue({ items: [structuredClone(sku)], total: 1 } as any)
  vi.mocked(fetchPlansPage).mockResolvedValue({ items: [structuredClone(plan)], total: 1 } as any)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })
async function open(query: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/plans', component: Plans }] })
  await router.push('/admin/plans?plan=1' + query)
  wrapper = shallowMount(Plans, { global: {
    plugins: [router], renderStubDefaultSlot: true,
    components: { UiCheckbox, UiButton: button },
    stubs: { SKUCycleFields: false, UiCheckbox: false, CheckboxRoot: false, UiButton: false, UiInput: true, UiTextarea: true, UiSelect: true, PageRefreshButton: true, UiNumberInput: false, FormField, ModalDialog: modal, DetailDrawer: modal },
  } })
  await flushPromises()
  return wrapper
}
it('allows clearing the last scene without silently restoring purchase', async () => {
  const view = await open('&sku=1')
  const operations = view.get('#sku-form .sku-operation-grid')
  expect(operations.findAll('[role="checkbox"]')).toHaveLength(3)
  const purchase = operations.get('[role="checkbox"]')
  await purchase.trigger('click')
  expect(purchase.attributes('aria-checked')).toBe('false')
  await view.get('#sku-form').trigger('submit'); await flushPromises()
  expect(updatePlanSKU).not.toHaveBeenCalled()
  expect(view.text()).toContain('至少选择')
  const change = operations.findAll('[role="checkbox"]')[2]
  await change.trigger('click')
  await view.get('#sku-form').trigger('submit'); await flushPromises()
  expect(updatePlanSKU).toHaveBeenCalledWith(1, expect.objectContaining({ allowed_operations: ['change'] }))
})
it('shows reset purpose once without disabled scenes or a fake execution count', async () => {
  vi.mocked(fetchPlanSKU).mockResolvedValue({ ...sku, entitlement_mode: 'traffic_reset', allowed_operations: ['reset'], billing_mode: 'one_time', billing_unit: 'once', billing_value: 1 } as any)
  const view = await open('&sku=1')
  const form = view.get('#sku-form')
  expect(form.find('.sku-operation-grid').exists()).toBe(false)
  expect(form.text()).not.toContain('周期数量')
  expect(form.text()).not.toContain('执行次数')
  expect(form.text()).toContain('到期时间和自动重置日期保持不变')
  await form.trigger('submit'); await flushPromises()
  expect(updatePlanSKU).toHaveBeenCalledWith(1, expect.objectContaining({ entitlement_mode: 'traffic_reset', allowed_operations: ['reset'], billing_value: 1 }))
})
it('saves false renewal and publication values', async () => {
  const view = await open('&editor=plan')
  await view.get('#edit-plan-is_renewable').trigger('click')
  await view.get('#edit-plan-is_active').trigger('click')
  await view.get('#edit-plan-form').trigger('submit'); await flushPromises()
  expect(updatePlan).toHaveBeenCalledWith(1, expect.objectContaining({ is_renewable: false, is_active: false }))
})
it('deletes an inactive SKU and reloads the counts', async () => {
  vi.mocked(fetchPlanSKUs).mockResolvedValue({ items: [{ ...sku, is_active: false }], total: 1 } as any)
  const view = await open('')
  await view.findAll('button').find(button => button.text() === '删除')!.trigger('click'); await flushPromises()
  expect(deletePlanSKU).toHaveBeenCalledWith(1)
  expect(fetchPlanDetail).toHaveBeenCalledTimes(2)
})
it('removes a product and closes its detail after confirmation', async () => {
  const view = await open('')
  await view.findAll('button').find(button => button.text() === '删除商品')!.trigger('click'); await flushPromises()
  expect(deletePlan).toHaveBeenCalledWith(1)
  expect(view.find('.detail-toolbar').exists()).toBe(false)
})
it('leaves catalog data intact when deletion confirmation is cancelled', async () => {
  vi.mocked(confirmAction).mockResolvedValue(false)
  const view = await open('')
  await view.findAll('button').find(button => button.text() === '删除商品')!.trigger('click'); await flushPromises()
  expect(deletePlan).not.toHaveBeenCalled()
})

it('uses a common cycle without overwriting the SKU name, code or scenes', async () => {
  const view = await open('&sku=1')
  view.get('#edit-sku-cycle').getComponent({name:'UiSelect'}).vm.$emit('update:modelValue', 'quarter')
  await flushPromises()
  expect(view.get('#sku-form').text()).not.toContain('周期数量')
  await view.get('#sku-form').trigger('submit'); await flushPromises()
  expect(updatePlanSKU).toHaveBeenCalledWith(1, expect.objectContaining({name: 'Month', code: 'starter-month', billing_unit: 'month', billing_value: 3, allowed_operations: ['purchase']}))
})
it('renders backend sales summaries without loading each listed product SKU', async () => {
  vi.mocked(fetchPlansPage).mockResolvedValue({items:[{...plan,sales_options:[{operation:'purchase',currency:'CNY',min_price_cents:1000,max_price_cents:4300},{operation:'reset',currency:'CNY',min_price_cents:500,max_price_cents:500}]}],total:1} as any)
  const view = await open('')
  const values = view.findAllComponents(TableText).map(item=>item.props('value'))
  expect(values).toContain('新购 · 重置流量');expect(values).toContain('新购 ¥10.00 起 · 重置流量 ¥5.00')
  expect(fetchPlanSKUs).toHaveBeenCalledTimes(1) // Only the explicitly opened detail.
})
