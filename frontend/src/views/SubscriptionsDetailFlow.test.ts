import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Subscriptions from './Subscriptions.vue'
import SubscriptionDetail from '../components/SubscriptionDetail.vue'
import UiSelect from '../components/UiSelect.vue'
import UiButton from '../components/UiButton.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import { fetchAdminSubscriptionDetail, fetchSubscriptionsPage, updateSubscriptionQuota } from '../api/client'
import { confirmAction } from '../utils/feedback'
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchAdminSubscriptionDetail: vi.fn(), fetchSubscriptionsPage: vi.fn(), updateSubscriptionQuota: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 1, isAdmin: true }, isAdmin: true, systemTimeZone: 'Asia/Shanghai' }) }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(), notify: vi.fn() }))
const sub = { id: 17, user_id: 22, plan_name: 'Pass Iron', sku_name: '月付', status: 'active', flow_total: 100, flow_used: 20, reset_quota_bytes: 100, reset_policy: 2, start_at: '2026-09-08T00:00:00Z', end_at: '2027-09-08T00:00:00Z', next_reset_at: null, active_credential_count: 1 } as any
const drawer = defineComponent({ props: ['open'], emits: ['close'], template: '<section v-if="open"><button aria-label="关闭订阅详情" @click="$emit(\'close\')">关闭</button><slot /></section>' })
const workbench = defineComponent({ template: '<section><slot name="filters" /><slot /><slot name="footer" /></section>' })
let wrapper: ReturnType<typeof mount>
beforeEach(() => { vi.clearAllMocks(); vi.mocked(fetchSubscriptionsPage).mockResolvedValue({ items: [], total: 0 } as any); vi.mocked(fetchAdminSubscriptionDetail).mockResolvedValue({ ...sub }); vi.mocked(updateSubscriptionQuota).mockResolvedValue(); vi.mocked(confirmAction).mockResolvedValue(true) })
afterEach(() => wrapper?.unmount())
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/subscriptions', component: Subscriptions }] })
  await router.push('/admin/subscriptions?user_id=22&status=active&return_to=%2Fadmin%2Fusers%3Fpage%3D2&subscription=17')
  wrapper = mount(defineComponent({ components: { RouterView }, template: '<RouterView />' }), { global: { plugins: [router], components: { UiButton, PageRefreshButton }, stubs: { DataWorkbench: workbench, DetailDrawer: drawer, TablePager: true } } })
  await flushPromises()
  return { router, view: wrapper }
}
it('guards switching objects and closing a dirty detail, preserving list filters and return context', async () => {
  const { router, view } = await open()
  await view.get('#subscription-edit-reset').trigger('click')
  view.getComponent(UiSelect).vm.$emit('update:modelValue', 5)
  await view.get('textarea').setValue('保留未保存设置')
  vi.mocked(confirmAction).mockResolvedValueOnce(false)
  await router.push({ query: { ...router.currentRoute.value.query, subscription: '18' } }); await flushPromises()
  expect(router.currentRoute.value.query.subscription).toBe('17')
  expect(fetchAdminSubscriptionDetail).toHaveBeenCalledTimes(1)
  await view.get('[aria-label="关闭订阅详情"]').trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ user_id: '22', status: 'active', return_to: '/admin/users?page=2' })
  expect(confirmAction).toHaveBeenCalledTimes(2)
  expect(view.findComponent(SubscriptionDetail).exists()).toBe(false)
  expect(updateSubscriptionQuota).not.toHaveBeenCalled()
})
it('keeps the read view mounted while refreshing after save', async () => {
  const { view } = await open()
  await view.get('#subscription-edit-reset').trigger('click')
  view.getComponent(UiSelect).vm.$emit('update:modelValue', 5)
  await view.get('textarea').setValue('停止后续自动重置')
  let finish!: (value: any) => void
  vi.mocked(fetchAdminSubscriptionDetail).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
  await view.get('form').trigger('submit'); await flushPromises()
  expect(view.find('.subscription-summary').exists()).toBe(true)
  expect(view.text()).not.toContain('正在加载订阅详情')
  expect(view.find('form').exists()).toBe(false)
  finish({ ...sub, reset_policy: 5 }); await flushPromises()
  expect(view.find('.subscription-summary').exists()).toBe(true)
  expect(view.text()).toContain('不重置')
})
