import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import SubscriptionDetail from './SubscriptionDetail.vue'
import SubscriptionQuotaEditor from './SubscriptionQuotaEditor.vue'
import ByteSizeInput from './ByteSizeInput.vue'
import UiSelect from './UiSelect.vue'
import { updateSubscriptionQuota } from '../api/client'
import { fetchAdministrativeSubscriptionValue, administrativelyTerminateSubscription } from '../api/subscriptionValue'
import { confirmAction } from '../utils/feedback'

vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), updateSubscriptionQuota: vi.fn() }))
vi.mock('../api/subscriptionValue', () => ({ fetchAdministrativeSubscriptionValue: vi.fn(), administrativelyTerminateSubscription: vi.fn(), fetchSubscriptionValue: vi.fn(), terminateSubscription: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 1, isAdmin: true }, isAdmin: true, systemTimeZone: 'Asia/Shanghai' }) }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(), notify: vi.fn() }))
const GB = 1024 ** 3
const sub = { id: 17, user_id: 22, user_email: 'customer@example.test', plan_id: 1, plan_name: 'Pass Iron', sku_name: '年付', status: 'active', start_at: '2026-09-08T07:24:49Z', end_at: '2027-09-08T07:24:49Z', flow_total: 100 * GB, flow_used: 25 * GB, reset_quota_bytes: 100 * GB, reset_policy: 2, next_reset_at: '2026-12-08T07:24:49Z', device_limit: 3, speed_limit_mbps: 0, traffic_calc_mode: 0, active_credential_count: 2, total_credential_count: 2 } as any
const modal = defineComponent({ props: ['open', 'title'], template: '<section v-if="open" role="dialog" :aria-label="title"><slot /></section>' })
let wrapper: ReturnType<typeof mount>
beforeEach(() => { vi.clearAllMocks(); localStorage.clear(); vi.mocked(confirmAction).mockResolvedValue(true); vi.mocked(updateSubscriptionQuota).mockResolvedValue() })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
async function open(subscription = sub) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
  await router.push('/admin/subscriptions?user_id=22&subscription=17')
  wrapper = mount(SubscriptionDetail, { attachTo: document.body, props: { subscription }, global: { plugins: [router], stubs: { ModalDialog: modal } } })
  await flushPromises()
  return wrapper
}
it('starts as a read view with quota progress and expiry, without forms or financial reads', async () => {
  const view = await open()
  expect(view.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('25')
  expect(view.text()).toContain('75.0 GB')
  expect(view.get('time').attributes('datetime')).toBe(sub.end_at)
  expect(view.find('form').exists()).toBe(false)
  expect(view.find('input').exists()).toBe(false)
  expect(view.text()).not.toContain('退款')
  expect(fetchAdministrativeSubscriptionValue).not.toHaveBeenCalled()
  expect(administrativelyTerminateSubscription).not.toHaveBeenCalled()
})
it('edits reset settings alone and keeps its baseline through a background read refresh', async () => {
  const view = await open()
  await view.get('#subscription-edit-reset').trigger('click')
  expect(view.findAllComponents(ByteSizeInput)).toHaveLength(1)
  view.getComponent(UiSelect).vm.$emit('update:modelValue', 5)
  await view.get('textarea').setValue('停用自动重置')
  await view.setProps({ subscription: { ...sub, flow_used: 30 * GB } })
  await view.get('form').trigger('submit'); await flushPromises()
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(17, { reset_policy: 5, reason: '停用自动重置', idempotency_key: expect.any(String) })
  expect(view.findComponent(SubscriptionQuotaEditor).exists()).toBe(false)
  expect(view.emitted('changed')).toHaveLength(1)
})
it('preserves an edited quota on save failure and asks before discarding it', async () => {
  const view = await open()
  await view.get('#subscription-edit-quota').trigger('click')
  expect(view.findAllComponents(ByteSizeInput)).toHaveLength(2)
  expect(view.findComponent(UiSelect).exists()).toBe(false)
  view.findAllComponents(ByteSizeInput)[0].vm.$emit('update:modelValue', 200 * GB)
  await view.get('textarea').setValue('增加本期额度')
  vi.mocked(updateSubscriptionQuota).mockRejectedValueOnce(new Error('network'))
  await view.get('form').trigger('submit'); await flushPromises()
  expect(view.find('form').exists()).toBe(true)
  expect(updateSubscriptionQuota).toHaveBeenCalledWith(17, expect.objectContaining({ flow_total: 200 * GB }))
  vi.mocked(confirmAction).mockResolvedValue(false)
  expect(await (view.vm as any).confirmLeave()).toBe(false)
  const cancel = view.findAll('button').find(button => button.text() === '取消')!
  await cancel.trigger('click'); await flushPromises()
  expect(view.find('form').exists()).toBe(true)
  vi.mocked(confirmAction).mockResolvedValue(true)
  await cancel.trigger('click'); await flushPromises()
  expect(view.find('form').exists()).toBe(false)
})
it('opens termination from the menu, reads its quote and requires a final explicit confirmation', async () => {
  vi.mocked(fetchAdministrativeSubscriptionValue).mockResolvedValue({ subscription_id: 17, user_id: 22, currency: 'CNY', current_minor: '200', future_minor: '0', total_minor: '200', max_refund_minor: '200', quote_fingerprint: 'a'.repeat(64) })
  const view = await open()
  await view.get('[data-row-action-trigger="subscription-detail-17"]').trigger('click'); await flushPromises()
  const retire = [...document.querySelectorAll<HTMLButtonElement>('button')].find(button => button.textContent === '退订订阅')!
  retire.click(); await flushPromises()
  expect(fetchAdministrativeSubscriptionValue).toHaveBeenCalledWith(17)
  expect(view.get('[role="dialog"]').attributes('aria-label')).toBe('退订 · Pass Iron')
  expect(administrativelyTerminateSubscription).not.toHaveBeenCalled()
  await view.findAll('input').at(-1)!.setValue('管理员核准退订')
  vi.mocked(confirmAction).mockResolvedValue(false)
  await view.findAll('button').find(button => button.text() === '确认终止订阅并退款至余额')!.trigger('click'); await flushPromises()
  expect(confirmAction).toHaveBeenCalledWith(expect.objectContaining({ confirmText: '确认退订', tone: 'danger' }))
  expect(administrativelyTerminateSubscription).not.toHaveBeenCalled()
  expect(localStorage.getItem('zboard:admin-unsubscribe:1:17')).toBeNull()
  vi.mocked(confirmAction).mockResolvedValue(true)
  vi.mocked(administrativelyTerminateSubscription).mockResolvedValue({ id: 9, subscription_id: 17, currency: 'CNY', amount_minor: '200', status: 'canceled', revocation_status: 'pending_verification', created_at: '2026-11-10T00:00:00Z' })
  await view.findAll('button').find(button => button.text() === '确认终止订阅并退款至余额')!.trigger('click'); await flushPromises()
  expect(administrativelyTerminateSubscription).toHaveBeenCalledWith(17, expect.objectContaining({ amount_minor: 200, reason: '管理员核准退订' }))
  expect(view.emitted('changed')).toHaveLength(1)
})
it('keeps ended subscriptions readable and hides quota mutations', async () => {
  const view = await open({ ...sub, status: 'ended', ended_at: '2026-11-01T00:00:00Z', can_renew: false })
  expect(view.find('#subscription-edit-quota').exists()).toBe(false)
  expect(view.find('#subscription-edit-reset').exists()).toBe(false)
  expect(view.text()).toContain('重置设置')
  expect(view.find('form').exists()).toBe(false)
})
