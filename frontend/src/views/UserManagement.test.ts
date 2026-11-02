import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Users from './Users.vue'
import UiButton from '../components/UiButton.vue'
import UiInput from '../components/UiInput.vue'
import UiSelect from '../components/UiSelect.vue'
import UiCheckbox from '../components/UiCheckbox.vue'
import FormField from '../components/FormField.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), update: vi.fn() }))
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchUsersPage: api.list, fetchAdminUserDetail: api.detail, updateAdminUser: api.update }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 1 }, token: 'fixture', reauthRequired: false }) }))
const user = { id: 22, email: 'customer@example.test', is_admin: false, status: 'active', active_subscription_count: 1, total_subscription_count: 2, pending_order_count: 0, total_order_count: 3, created_at: '2026-11-01', wallet_balances: [{ currency: 'CNY', balance_minor: '12345', reserved_minor: '0' }], cleanup: { enabled: true, inactive_days: 14, state: 'scheduled', eligible_at: '2026-11-15T00:00:00Z', reason: '从注册或最后一份订阅结束日起计算' } }
let wrapper: VueWrapper | undefined
beforeEach(() => { vi.resetAllMocks(); api.list.mockResolvedValue({ items: [user], total: 1 }); api.detail.mockResolvedValue(user); api.update.mockResolvedValue(user) })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/users', component: Users }] })
  await router.push('/admin/users?q=customer'); await router.isReady()
  wrapper = mount(Users, { attachTo: document.body, global: {
    plugins: [router], components: { UiButton, UiInput, UiSelect, UiCheckbox, FormField },
    stubs: {
      AdminWalletPanel: true, PluginSlot: true,
      DetailDrawer: defineComponent({ props: ['open', 'title'], template: '<aside v-if="open" data-user-details><h2>{{ title }}</h2><slot/></aside>' }),
    },
  } })
  await flushPromises()
  return { wrapper, router }
}
function dialog() { return document.body.querySelector<HTMLElement>('.app-dialog')! }
async function submitPassword(password: string) {
  const input = dialog().querySelector<HTMLInputElement>('input')!
  input.value = password; input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
  dialog().querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flushPromises()
}
async function openListPasswordReset(view: VueWrapper) {
  await view.get('[data-row-action-trigger="user-22"]').trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  document.body.querySelector<HTMLElement>('[data-user-password-trigger="list-22"]')!.click()
  await flushPromises()
}

it('keeps status, balances and cleanup visible with low-frequency actions in one menu', async () => {
  const { wrapper, router } = await render()
  expect(wrapper.find('input[aria-label="搜索用户"]').exists()).toBe(true)
  expect(wrapper.find('[data-user-edit-trigger="list-22"]').exists()).toBe(false)
  expect(wrapper.get('tbody').text()).toContain('CNY 123.45')
  expect(wrapper.get('tbody time').attributes('datetime')).toBe(user.cleanup.eligible_at)
  expect(wrapper.get('tbody [role="switch"]').attributes('aria-checked')).toBe('true')
  expect(wrapper.find('thead [data-column-priority="2"], thead [data-column-priority="3"]').exists()).toBe(false)
  expect(wrapper.find('[data-user-password-trigger="list-22"]').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('删除用户')
  await wrapper.get('[data-user-detail-trigger="22"]').trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query).toMatchObject({ q: 'customer', user: '22' })
  expect(wrapper.get('[data-user-details] .user-detail-actions').text()).not.toContain('重置密码')
  expect(wrapper.get('[aria-label="账户安全"]').text()).toContain('重置密码')
  expect(wrapper.get('[data-user-details] .user-detail-actions').findAll('button')).toHaveLength(2)
  expect(wrapper.find('admin-wallet-panel-stub').exists()).toBe(false)
  await wrapper.get('[role="tab"][data-state="inactive"]').trigger('mousedown', { button: 0 })
  await flushPromises()
  expect(wrapper.find('admin-wallet-panel-stub').exists()).toBe(true)
  expect(api.detail).toHaveBeenCalledTimes(1)
})

it('resets only the selected password, shows failure in place and clears the secret after success', async () => {
  const { wrapper } = await render()
  await openListPasswordReset(wrapper)
  expect(dialog().textContent).toContain(user.email)
  expect(document.activeElement?.id).toBe('reset-user-password')
  expect(api.detail).not.toHaveBeenCalled()
  api.update.mockRejectedValueOnce({ response: { data: { message: '服务暂不可用，请重试。' } } })
  await submitPassword('test-password-only')
  expect(api.update).toHaveBeenLastCalledWith(22, { password: 'test-password-only' })
  expect(dialog().textContent).toContain('服务暂不可用，请重试。')
  await submitPassword('test-password-only')
  expect(document.body.querySelector('.app-dialog')).toBeNull()
  expect(api.list).toHaveBeenCalledTimes(1)
  await openListPasswordReset(wrapper)
  expect(dialog().querySelector<HTMLInputElement>('input')?.value).toBe('')
})

it('keeps invalid passwords beside the field and prevents duplicate submission', async () => {
  const { wrapper } = await render()
  await openListPasswordReset(wrapper)
  await submitPassword('short')
  expect(api.update).not.toHaveBeenCalled()
  expect(dialog().textContent).toContain('密码必须为 12–72 个 UTF-8 字节')
  let resolve!: (value: unknown) => void
  api.update.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const input = dialog().querySelector<HTMLInputElement>('input')!
  input.value = 'valid-fixture-password'; input.dispatchEvent(new Event('input', { bubbles: true }))
  const form = dialog().querySelector('form')!
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await flushPromises()
  expect(api.update).toHaveBeenCalledTimes(1)
  resolve(user); await flushPromises()
})


it('saves status directly, blocks duplicate writes and keeps a rejected change beside its switch', async () => {
  const { wrapper } = await render()
  const toggle = wrapper.get('tbody [role="switch"]')
  let reject!: (cause: unknown) => void
  api.update.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail }))
  await toggle.trigger('click'); await flushPromises()
  expect(api.update).toHaveBeenLastCalledWith(22, { status: 'suspended' })
  expect(toggle.attributes('disabled')).toBeDefined()
  expect(toggle.attributes('aria-checked')).toBe('true')
  await toggle.trigger('click'); expect(api.update).toHaveBeenCalledTimes(1)
  reject({ response: { data: { message: '不能停用最后一个管理员' } } }); await flushPromises()
  expect(wrapper.get('tbody [role="alert"]').text()).toContain('不能停用最后一个管理员')
  expect(toggle.attributes('aria-checked')).toBe('true')
  api.update.mockResolvedValueOnce({ ...user, status: 'suspended' })
  await toggle.trigger('click'); await flushPromises()
  expect(toggle.attributes('aria-checked')).toBe('false')
  expect(wrapper.find('tbody [role="alert"]').exists()).toBe(false)
})

it('opens the wallet directly from the balance without the extra tab selection', async () => {
  const { wrapper, router } = await render()
  await wrapper.get('.user-wallet-link').trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query).toMatchObject({ user: '22', section: 'wallet', wallet_currency: 'CNY' })
  expect(wrapper.get('admin-wallet-panel-stub').attributes('initialcurrency')).toBe('CNY')
  expect(wrapper.find('admin-wallet-panel-stub').exists()).toBe(true)
})
