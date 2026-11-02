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
const user = { id: 22, email: 'customer@example.test', is_admin: false, status: 'active', active_subscription_count: 1, total_subscription_count: 2, pending_order_count: 0, total_order_count: 3, created_at: '2026-11-01' }
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

it('shows search, edit and reset directly and opens details from the user identity', async () => {
  const { wrapper, router } = await render()
  expect(wrapper.find('input[aria-label="搜索用户"]').exists()).toBe(true)
  expect(wrapper.get('[data-user-edit-trigger="list-22"]').text()).toBe('编辑')
  expect(wrapper.get('[data-user-password-trigger="list-22"]').text()).toBe('重置密码')
  expect(wrapper.text()).not.toContain('删除用户')
  await wrapper.get('[data-user-detail-trigger="22"]').trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query).toMatchObject({ q: 'customer', user: '22' })
  expect(wrapper.get('[data-user-details] .user-detail-actions').text()).toContain('重置密码')
  expect(wrapper.find('admin-wallet-panel-stub').exists()).toBe(false)
  await wrapper.get('[role="tab"][data-state="inactive"]').trigger('mousedown', { button: 0 })
  await flushPromises()
  expect(wrapper.find('admin-wallet-panel-stub').exists()).toBe(true)
  expect(api.detail).toHaveBeenCalledTimes(1)
})

it('resets only the selected password, shows failure in place and clears the secret after success', async () => {
  const { wrapper } = await render()
  await wrapper.get('[data-user-password-trigger="list-22"]').trigger('click'); await flushPromises()
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
  await wrapper.get('[data-user-password-trigger="list-22"]').trigger('click'); await flushPromises()
  expect(dialog().querySelector<HTMLInputElement>('input')?.value).toBe('')
})

it('keeps invalid passwords beside the field and prevents duplicate submission', async () => {
  const { wrapper } = await render()
  await wrapper.get('[data-user-password-trigger="list-22"]').trigger('click'); await flushPromises()
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
