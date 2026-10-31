import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Users from './Users.vue'
import FormField from '../components/FormField.vue'
import UiInput from '../components/UiInput.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), preview: vi.fn(), remove: vi.fn(), app: { token: 'verified', user: { id: 1 }, reauthRequired: false, sessionEpoch: 0 } }))
vi.mock('../stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchUsersPage: mocks.list, fetchUserDeletionPreview: mocks.preview, deleteAdminUser: mocks.remove }))
const user = { id: 2, email: 'owner@example.test', status: 'active', is_admin: false, created_at: '2026-10-01', active_subscription_count: 0 }
const modal = defineComponent({ props: ['open', 'title'], template: '<section v-if="open" role="dialog"><h2>{{ title }}</h2><slot/><slot name="footer" :request-close="()=>{}"/></section>' })
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  mocks.list.mockResolvedValue({ items: [user], total: 1 })
  mocks.preview.mockResolvedValue({ user_id: 2, allowed: true, blockers: [], history_retained: true })
  mocks.remove.mockResolvedValue(undefined)
})
afterEach(() => { wrapper?.unmount() })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/users', component: Users }] })
  await router.push('/admin/users')
  wrapper = shallowMount(Users, { global: { plugins: [router], components: { FormField, UiInput }, renderStubDefaultSlot: true, stubs: {
    ModalDialog: modal, DetailDrawer: true, UiInput: false, FormField: false,
    UiButton: defineComponent({ template: '<button><slot/></button>' }),
    PageAlert: { template: '<aside><slot/></aside>' },
    PageHeader: { template: '<header><slot name="actions"/></header>' },
    DataTable: { template: '<table><slot/></table>' },
  } } })
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === '删除用户')!.trigger('click')
  await flushPromises()
  return wrapper
}
it('shows blocking reasons at the deletion action without allowing a delete', async () => {
  mocks.preview.mockResolvedValue({ user_id: 2, allowed: false, blockers: ['钱包仍有余额或冻结资金，请先完成结算。'], history_retained: true })
  const w = await render()
  expect(w.get('[role="dialog"]').text()).toContain('钱包仍有余额')
  expect(w.get('[role="dialog"]').findAll('button').find(button => button.text() === '永久删除')!.attributes('disabled')).toBeDefined()
  expect(mocks.remove).not.toHaveBeenCalled()
})
it('requires the target email, keeps a failed delete in the dialog, and refreshes after success', async () => {
  const w = await render()
  const dialog = w.get('[role="dialog"]')
  const remove = () => dialog.findAll('button').find(button => button.text() === '永久删除')!
  expect(remove().attributes('disabled')).toBeDefined()
  await dialog.get('input').setValue(user.email)
  mocks.remove.mockRejectedValueOnce({ response: { data: { message: '仍有未结订单，请先取消或完成履约。' } } })
  await remove().trigger('click'); await flushPromises()
  expect(dialog.text()).toContain('仍有未结订单')
  expect(mocks.remove).toHaveBeenCalledWith(2)
  await remove().trigger('click'); await flushPromises()
  expect(w.find('[role="dialog"]').exists()).toBe(false)
  expect(mocks.list).toHaveBeenCalledTimes(2)
})
