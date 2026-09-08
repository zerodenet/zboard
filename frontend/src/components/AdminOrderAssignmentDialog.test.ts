import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminOrderAssignmentDialog from './AdminOrderAssignmentDialog.vue'
import AdminOrderLookup from './AdminOrderLookup.vue'
import MoneyInput from './MoneyInput.vue'
import UiSelect from './UiSelect.vue'
const mocks = vi.hoisted(() => ({ assign: vi.fn(), user: vi.fn(), users: vi.fn(), targets: vi.fn(), plans: vi.fn(), skus: vi.fn() }))
vi.mock('../api/client', () => ({ assignAdminOrder: mocks.assign, fetchAdminUserDetail: mocks.user, fetchUsersPage: mocks.users, fetchSubscriptionsPage: mocks.targets, fetchPlanCatalogPage: mocks.plans, fetchPlanCatalogSKUs: mocks.skus }))
const modal = defineComponent({ template: '<div><slot /><slot name="footer" /></div>' })
const lookup = defineComponent({ props: ['modelValue', 'inputId', 'fetchPage'], emits: ['update:modelValue'], template: '<div />' })
function render() { return mount(AdminOrderAssignmentDialog, { props: { open: true, userId: 7 }, global: { plugins: [PrimeVue], stubs: { ModalDialog: modal, AdminOrderLookup: lookup } } }) }
function findLookup(wrapper: ReturnType<typeof render>, id: string) { return wrapper.findAllComponents(AdminOrderLookup).find(item => item.props('inputId') === id)! }
async function fill(wrapper: ReturnType<typeof render>) {
  await flushPromises()
  findLookup(wrapper, 'assign-plan').vm.$emit('update:modelValue', { id: 12, label: '月付套餐' }); await flushPromises()
  findLookup(wrapper, 'assign-sku').vm.$emit('update:modelValue', { id: 18, label: '月付', sku: { price_cents: 2000, currency: 'CNY' } }); await flushPromises()
  await wrapper.get('textarea').setValue('按客户要求代下单')
}
beforeEach(() => {
  vi.resetAllMocks()
  mocks.user.mockResolvedValue({ id: 7, email: 'buyer@example.com' })
  mocks.assign.mockResolvedValue({ id: 81, user_id: 7, payable_amount: 1500, status: 'pending' })
  for (const mock of [mocks.users, mocks.targets, mocks.plans, mocks.skus]) mock.mockResolvedValue({ items: [], total: 0 })
})
describe('administrator order assignment', () => {
  it('prefills the recipient, defaults the SKU price and submits the edited amount without granting rights', async () => {
    const wrapper = render(); await fill(wrapper)
    expect(findLookup(wrapper, 'assign-user').props('modelValue')?.id).toBe(7)
    expect(wrapper.getComponent(MoneyInput).props('modelValue')).toBe(2000)
    wrapper.getComponent(MoneyInput).vm.$emit('update:modelValue', 1500); await flushPromises()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.assign).toHaveBeenCalledWith({ user_id: 7, plan_sku_id: 18, target_subscription_id: undefined, payable_amount: 1500, note: '按客户要求代下单', request_id: expect.any(String) })
    expect(wrapper.emitted('assigned')?.[0]?.[0]).toMatchObject({ id: 81, status: 'pending' })
    expect(wrapper.text()).toContain('创建待付款订单'); expect(wrapper.text()).not.toContain('免费开通')
    wrapper.unmount()
  })
  it('reuses the request ID after a network failure and changes it only for a different payload', async () => {
    mocks.assign.mockRejectedValue(new Error('connection lost'))
    const wrapper = render(); await fill(wrapper)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.assign.mock.calls[1][0].request_id).toBe(mocks.assign.mock.calls[0][0].request_id)
    wrapper.getComponent(MoneyInput).vm.$emit('update:modelValue', 0); await flushPromises()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.assign.mock.calls[2][0].request_id).not.toBe(mocks.assign.mock.calls[1][0].request_id)
    expect(mocks.assign.mock.calls[2][0].payable_amount).toBe(0)
    wrapper.unmount()
  })
  it('clears the SKU and edited price when changing plans', async () => {
    const wrapper = render(); await fill(wrapper)
    wrapper.getComponent(MoneyInput).vm.$emit('update:modelValue', 1500); await flushPromises()
    findLookup(wrapper, 'assign-plan').vm.$emit('update:modelValue', { id: 13, label: '另一套餐' }); await flushPromises()
    expect(findLookup(wrapper, 'assign-sku').props('modelValue')).toBeNull()
    expect(wrapper.getComponent(MoneyInput).props('modelValue')).toBe(0)
    await wrapper.get('form').trigger('submit'); expect(mocks.assign).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('queries only the selected user’s subscriptions and clears them when changing users', async () => {
    const wrapper = render(); await flushPromises()
    wrapper.getComponent(UiSelect).vm.$emit('update:modelValue', 'renew'); await flushPromises()
    const target = findLookup(wrapper, 'assign-target')
    await target.props('fetchPage')('月付', 25, new AbortController().signal)
    expect(mocks.targets).toHaveBeenCalledWith(expect.objectContaining({ userId: 7, q: '月付', offset: 25, limit: 25 }), expect.anything())
    target.vm.$emit('update:modelValue', { id: 30, label: '订阅', planId: 12 }); await flushPromises()
    await fill(wrapper)
    findLookup(wrapper, 'assign-user').vm.$emit('update:modelValue', { id: 8, label: 'another@example.com' }); await flushPromises()
    expect(findLookup(wrapper, 'assign-target').props('modelValue')).toBeNull()
    expect(findLookup(wrapper, 'assign-plan').props('modelValue')).toBeNull()
    expect(findLookup(wrapper, 'assign-sku').props('modelValue')).toBeNull()
    wrapper.unmount()
  })
  it('prevents a second in-flight submission', async () => {
    let resolve!: (value: unknown) => void
    mocks.assign.mockImplementation(() => new Promise(done => { resolve = done }))
    const wrapper = render(); await fill(wrapper)
    await wrapper.get('form').trigger('submit'); await wrapper.get('form').trigger('submit')
    expect(mocks.assign).toHaveBeenCalledTimes(1)
    resolve({ id: 81 }); await flushPromises(); wrapper.unmount()
  })
})
