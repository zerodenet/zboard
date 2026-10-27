import { defineComponent, h } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Plans from './Plans.vue'
import FormField from '../components/FormField.vue'
import { createPlan, createPlanSKU, fetchPlanDetail, fetchPlanSKU, fetchPlanSKUs, fetchPlansPage, updatePlan, updatePlanSKU } from '../api/client'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }) }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn(async () => ({ revision: 1, page_available: true, nodes: [] })) }))
vi.mock('../api/client', () => ({ createPlan: vi.fn(), createPlanSKU: vi.fn(), fetchPlanDetail: vi.fn(), fetchPlanSKU: vi.fn(), fetchPlanSKUs: vi.fn(), fetchPlansPage: vi.fn(), updatePlan: vi.fn(), updatePlanSKU: vi.fn(), deletePlan: vi.fn(), deletePlanSKU: vi.fn() }))
vi.mock('../utils/feedback', () => ({ notify: vi.fn(), confirmAction: vi.fn(async () => true) }))
const plan = { id: 1, name: 'Server plan', slug: 'server-plan', summary: '', description: '', node_group_id: 1, traffic_bytes: 1024 ** 3, speed_limit_mbps: 0, max_active_subscriptions: 0, is_renewable: true, device_limit: 1, family_limit: 0, reset_policy: 2, traffic_calc_mode: 0, is_active: true, sort_order: 0, revision: 1, sku_count: 1, active_sku_count: 1 }
const sku = { id: 1, plan_id: 1, code: 'server-month', name: 'Month', sku_type: 'new', billing_mode: 'periodic', entitlement_mode: 'plan', renewal_effect: 'extend_only', allowed_operations: ['purchase', 'renew'], billing_unit: 'month', billing_value: 1, currency: 'CNY', price_cents: 1000, is_active: true, sort_order: 0, grant_traffic_bytes: 0, updated_at: '2026-10-27T01:00:00Z' }
const input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value) }) } })
const number = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup(props, { emit }) { return () => h('input', { value: props.modelValue, onInput: (event: Event) => emit('update:modelValue', Number((event.target as HTMLInputElement).value)) }) } })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts()
  vi.mocked(fetchPlanDetail).mockResolvedValue(structuredClone(plan) as any)
  vi.mocked(fetchPlanSKU).mockResolvedValue(structuredClone(sku) as any)
  vi.mocked(fetchPlansPage).mockResolvedValue({ items: [structuredClone(plan)], total: 1 } as any)
  vi.mocked(fetchPlanSKUs).mockResolvedValue({ items: [structuredClone(sku)], total: 1 } as any)
})
afterEach(() => { view?.unmount(); clearReauthenticationDrafts() })
async function open(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/plans', component: Plans }] })
  await router.push(path)
  view = shallowMount(Plans, { global: { plugins: [router], renderStubDefaultSlot: true,
    components: { UiInput: input, UiTextarea: input, UiNumberInput: number, UiButton: defineComponent({ template: '<button><slot /></button>' }) },
    stubs: { UiInput: false, UiTextarea: false, UiNumberInput: false, UiButton: false, MoneyInput: number, FormField, ModalDialog: modal, DetailDrawer: modal, PageHeader: { template: '<header><slot /><slot name="actions" /></header>' }, PageAlert: { template: '<aside><slot /><slot name="actions" /></aside>' } },
  } })
  await flushPromises()
  return router
}
async function roundTrip(path: string) {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  view!.unmount(); view = undefined
  confirmReauthenticationDraftIdentity(1, true)
  await open(path)
}
it('restores a Plan draft with its original version and never submits on recovery', async () => {
  const path = '/admin/plans?plan=1&editor=plan'
  await open(path); await view!.get('#edit-plan-name').setValue('Unsaved operator title')
  await roundTrip(path)
  expect((view!.get('#edit-plan-name').element as HTMLInputElement).value).toBe('Unsaved operator title')
  expect(updatePlan).not.toHaveBeenCalled()
  await view!.get('#edit-plan-form').trigger('submit'); await flushPromises()
  expect(updatePlan).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Unsaved operator title', expected_revision: 1, sync_traffic_quota: false }))
})
it('shows the old Plan draft but blocks even programmatic submit after a concurrent server revision', async () => {
  const path = '/admin/plans?plan=1&editor=plan'
  await open(path); await view!.get('#edit-plan-name').setValue('Old unsaved edit')
  vi.mocked(fetchPlanDetail).mockResolvedValue({ ...plan, name: 'Changed elsewhere', revision: 2 } as any)
  await roundTrip(path)
  expect((view!.get('#edit-plan-name').element as HTMLInputElement).value).toBe('Old unsaved edit')
  expect(view!.text()).toContain('当前草稿基于旧版本')
  await view!.get('#edit-plan-form').trigger('submit'); await flushPromises()
  expect(updatePlan).not.toHaveBeenCalled()
})
it('restores first-SKU fields in an unsubmitted new Plan', async () => {
  const path = '/admin/plans'
  await open(path); await view!.findAll('button').find(button => button.text().includes('创建商品'))!.trigger('click')
  await view!.get('#create-plan-name').setValue('New unsaved product')
  await view!.get('#create-plan-sku-name').setValue('Annual')
  await view!.get('#create-plan-sku-code').setValue('annual')
  await view!.get('#create-plan-price').setValue('12000')
  await roundTrip(path)
  expect((view!.get('#create-plan-sku-name').element as HTMLInputElement).value).toBe('Annual')
  expect((view!.get('#create-plan-price').element as HTMLInputElement).value).toBe('12000')
  expect(createPlan).not.toHaveBeenCalled(); expect(createPlanSKU).not.toHaveBeenCalled()
})
it('blocks a recovered existing SKU when the fresh server baseline changed', async () => {
  const path = '/admin/plans?plan=1&sku=1'
  await open(path); await view!.get('#edit-sku-code').setValue('unsaved-sku')
  vi.mocked(fetchPlanSKU).mockResolvedValue({ ...sku, price_cents: 2000, updated_at: '2026-10-27T02:00:00Z' } as any)
  await roundTrip(path)
  expect((view!.get('#edit-sku-code').element as HTMLInputElement).value).toBe('unsaved-sku')
  expect(view!.text()).toContain('不能直接覆盖服务器的新内容')
  await view!.get('#sku-form').trigger('submit'); await flushPromises()
  expect(updatePlanSKU).not.toHaveBeenCalled()
})
