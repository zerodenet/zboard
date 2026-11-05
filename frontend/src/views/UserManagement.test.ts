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
  expect(wrapper.find('input[aria-label="搜索用户"]').exists()).toBe(false)
  expect(wrapper.get('.workbench-filter-chip-trigger').text()).toContain('customer')
  expect(wrapper.find('[data-user-edit-trigger="list-22"]').exists()).toBe(false)
  expect(wrapper.get('thead').text()).toContain('余额（CNY）')
  expect(wrapper.get('.user-wallet-link').text()).toBe('123.45')
  expect(wrapper.get('.user-wallet-link').attributes('aria-label')).toContain('CNY 余额')
  expect(wrapper.get('tbody time').attributes('datetime')).toBe(user.cleanup.eligible_at)
  expect(wrapper.get('tbody [role="switch"]').attributes('aria-checked')).toBe('true')
  expect(wrapper.get('tbody tr td:nth-child(2)').find('.status-badge').exists()).toBe(false)
  expect(wrapper.get('tbody [role="switch"]').attributes('aria-label')).toBe(`启用账户 ${user.email}`)
  expect(wrapper.get('tbody [role="switch"]').attributes('aria-describedby')).toBe('user-status-22')
  expect(wrapper.get('#user-status-22').classes()).toContain('sr-only')
  expect(wrapper.get('#user-status-22').text()).toBe('正常')
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

it('opens search from a capsule, applies the draft once and preserves the query in the URL', async () => {
  const { wrapper, router } = await render()
  const trigger = wrapper.get('.workbench-filter-chip-trigger')
  await trigger.trigger('click'); await flushPromises()
  const input = document.body.querySelector<HTMLInputElement>('input[aria-label="搜索用户"]')!
  expect(input.value).toBe('customer')
  input.value = '  new-customer  '
  input.dispatchEvent(new Event('input', { bubbles: true })); await flushPromises()
  expect(api.list).toHaveBeenCalledTimes(1)
  input.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter', bubbles: true })); await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('new-customer')
  expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ q: 'new-customer' }), expect.anything())
  expect(document.body.querySelector('.workbench-filter-popover')).toBeNull()
  expect(document.activeElement).toBe(trigger.element)
})

it('edits and clears the admin-only note through the account editor and refreshes the row', async () => {
  const noted = { ...user, admin_note: '研发团队 · 张三', updated_at: '2026-11-03T00:00:00Z' }
  api.list.mockImplementation(async () => ({ items: [{ ...noted }], total: 1 }))
  api.detail.mockImplementation(async () => ({ ...noted }))
  const { wrapper } = await render()
  expect(wrapper.get('tbody .user-admin-note').text()).toBe(noted.admin_note)
  expect(wrapper.get('[data-user-detail-trigger="22"]').text()).toBe(user.email)
  for (const note of ['采购团队 · 李四', '']) {
    await wrapper.get('[data-row-action-trigger="user-22"]').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    document.body.querySelector<HTMLElement>('[data-user-edit-trigger="list-22"]')!.click()
    await flushPromises()
    const input = document.body.querySelector<HTMLTextAreaElement>('#edit-user-note')!
    expect(input.value).toBe(noted.admin_note)
    input.value = note; input.dispatchEvent(new Event('input', { bubbles: true }))
    api.update.mockImplementationOnce(async (id, fields) => {
      expect(id).toBe(22)
      expect(fields).toEqual({ status: 'active', is_admin: false, admin_note: note })
      noted.admin_note = note
      return noted
    })
    dialog().querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.body.querySelector('.app-dialog')).toBeNull()
    expect(wrapper.find('tbody .user-admin-note').exists()).toBe(Boolean(note))
    if (note) expect(wrapper.get('tbody .user-admin-note').text()).toBe(note)
  }
  expect(api.update).toHaveBeenCalledTimes(2)
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
  expect(toggle.attributes('aria-busy')).toBe('true')
  expect(wrapper.get('#user-status-22').text()).toBe('保存中')
  expect(toggle.attributes('aria-checked')).toBe('true')
  await toggle.trigger('click'); expect(api.update).toHaveBeenCalledTimes(1)
  reject({ response: { data: { message: '不能停用最后一个管理员' } } }); await flushPromises()
  expect(wrapper.get('tbody [role="alert"]').text()).toContain('不能停用最后一个管理员')
  expect(toggle.attributes('aria-describedby')).toBe('user-status-22 user-status-error-22')
  expect(toggle.attributes('aria-busy')).toBe('false')
  expect(toggle.attributes('aria-checked')).toBe('true')
  api.update.mockResolvedValueOnce({ ...user, status: 'suspended' })
  await toggle.trigger('click'); await flushPromises()
  expect(toggle.attributes('aria-checked')).toBe('false')
  expect(toggle.attributes('title')).toBe('已暂停')
  expect(wrapper.get('#user-status-22').text()).toBe('已暂停')
  expect(wrapper.find('tbody [role="alert"]').exists()).toBe(false)
})

it('keeps deactivated accounts unchecked while exposing their distinct state accessibly', async () => {
  api.list.mockResolvedValueOnce({ items: [{ ...user, status: 'deactivated' }], total: 1 })
  const { wrapper } = await render()
  const toggle = wrapper.get('tbody [role="switch"]')
  expect(toggle.attributes('aria-checked')).toBe('false')
  expect(toggle.attributes('title')).toBe('已停用')
  expect(wrapper.get('#user-status-22').text()).toBe('已停用')
  expect(wrapper.get('#user-status-22').classes()).toContain('sr-only')
})

it('labels each balance only when multiple currencies share the list and opens the selected currency', async () => {
  api.list.mockResolvedValueOnce({ items: [{ ...user, wallet_balances: [
    ...user.wallet_balances,
    { currency: 'USD', balance_minor: '5678', reserved_minor: '200' },
  ] }], total: 1 })
  const { wrapper, router } = await render()
  expect(wrapper.get('thead th.numeric-column').text()).toBe('余额')
  const balances = wrapper.findAll('.user-wallet-link')
  expect(balances[0]!.text()).toBe('CNY 123.45')
  expect(balances[1]!.text()).toContain('USD 56.78')
  expect(balances[1]!.text()).toContain('含冻结 USD 2.00')
  await balances[1]!.trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query).toMatchObject({ user: '22', section: 'wallet', wallet_currency: 'USD' })
  expect(wrapper.get('admin-wallet-panel-stub').attributes('initialcurrency')).toBe('USD')
})

it('keeps unknown balances distinct from zero and retains integer precision', async () => {
  api.list.mockResolvedValueOnce({ items: [
    { ...user, wallet_balances: [{ currency: 'CNY', balance_minor: '900719925474099123', reserved_minor: '100' }] },
    { ...user, id: 23, email: 'unknown@example.test', wallet_balances: undefined },
    { ...user, id: 24, email: 'empty@example.test', wallet_balances: [] },
    { ...user, id: 25, email: 'zero@example.test', wallet_balances: [{ currency: 'CNY', balance_minor: '0', reserved_minor: '0' }] },
  ], total: 4 })
  const { wrapper } = await render()
  const balances = wrapper.findAll('.user-wallet-link')
  expect(balances[0]!.text()).toContain('9007199254740991.23')
  expect(balances[0]!.text()).toContain('含冻结 1.00')
  expect(balances[0]!.text()).not.toContain('CNY')
  expect(balances[1]!.text()).toBe('-')
  expect(balances[1]!.attributes('aria-label')).toContain('未知')
  expect(balances[2]!.text()).toBe('-')
  expect(balances[2]!.attributes('aria-label')).toContain('0.00')
  expect(balances[3]!.text()).toBe('-')
  expect(balances[3]!.attributes('aria-label')).toContain('CNY 0.00')
})

it('opens the wallet directly from the balance without the extra tab selection', async () => {
  const { wrapper, router } = await render()
  await wrapper.get('.user-wallet-link').trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query).toMatchObject({ user: '22', section: 'wallet', wallet_currency: 'CNY' })
  expect(wrapper.get('admin-wallet-panel-stub').attributes('initialcurrency')).toBe('CNY')
  expect(wrapper.find('admin-wallet-panel-stub').exists()).toBe(true)
})

it('refreshes the list balance and account protection details after a wallet change', async () => {
 const { wrapper }=await render()
 await wrapper.get('.user-wallet-link').trigger('click');await flushPromises()
 expect(api.detail).toHaveBeenCalledTimes(1)
 const changed={...user,wallet_balances:[{currency:'CNY',balance_minor:'22345',reserved_minor:'0'}],cleanup:{...user.cleanup,state:'protected',eligible_at:undefined,reason:'账户有余额'}}
 api.list.mockResolvedValue({items:[changed],total:1})
 let finish!:(value:unknown)=>void;api.detail.mockReturnValueOnce(new Promise(resolve=>{finish=resolve}))
 const panel=wrapper.getComponent({name:'AdminWalletPanel'}), identity=panel.vm.$.uid
 panel.vm.$emit('changed');await flushPromises()
 expect(wrapper.getComponent({name:'AdminWalletPanel'}).vm.$.uid).toBe(identity)
 finish(changed);await flushPromises()
 expect(api.list).toHaveBeenCalledTimes(2);expect(api.detail).toHaveBeenCalledTimes(2)
 expect(wrapper.get('.user-wallet-link').text()).toBe('223.45')
})


it('keeps the wallet panel mounted when the first currency balance appears after an adjustment', async () => {
  const empty = { ...user, wallet_balances: [] }
  api.detail.mockResolvedValueOnce(empty)
  const { wrapper } = await render()
  await wrapper.get('[data-user-detail-trigger="22"]').trigger('click'); await flushPromises()
  await wrapper.get('[role="tab"][data-state="inactive"]').trigger('mousedown', { button: 0 }); await flushPromises()
  const panel = wrapper.getComponent({ name: 'AdminWalletPanel' })
  const identity = panel.vm.$.uid
  const changed = { ...user, wallet_balances: [{ currency: 'EUR', balance_minor: '1000', reserved_minor: '0' }] }
  api.detail.mockResolvedValueOnce(changed)
  api.list.mockResolvedValueOnce({ items: [changed], total: 1 })
  panel.vm.$emit('changed'); await flushPromises()
  expect(wrapper.getComponent({ name: 'AdminWalletPanel' }).vm.$.uid).toBe(identity)
  expect(wrapper.get('.user-wallet-link').text()).toBe('10.00')
})
