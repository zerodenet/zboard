import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import SubscriptionTemplates from './SubscriptionTemplates.vue'
import { fetchSubscriptionTemplate, fetchSubscriptionTemplatesPage, updateSubscriptionTemplate } from '../api/client'
import { confirmAction } from '../utils/feedback'

vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: 'local-fixture', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }) }))
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchSubscriptionTemplate: vi.fn(), fetchSubscriptionTemplatesPage: vi.fn(), updateSubscriptionTemplate: vi.fn() }))
vi.mock('../utils/feedback', () => ({ confirmAction: vi.fn(), notify: vi.fn() }))
const item = { id: 1, revision: 3, name: 'Clash', slug: 'clash', description: '', renderer: 'clash' as const, is_active: true, sort_order: 0, created_at: '', updated_at: '' }
let wrapper: VueWrapper | undefined
beforeEach(() => { vi.resetAllMocks(); vi.mocked(fetchSubscriptionTemplatesPage).mockResolvedValue({ items: [item], total: 1 } as any) })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/subscription-templates', component: SubscriptionTemplates }] })
  await router.push('/admin/subscription-templates'); await router.isReady()
  wrapper = mount(SubscriptionTemplates, { attachTo: document.body, global: { plugins: [router], stubs: { SubscriptionTemplateCustomizer: true } } })
  await flushPromises()
  return wrapper
}

it('preserves the stop confirmation and guards a pending save while using the fresh revision', async () => {
  const view = await render(), toggle = view.get('tbody [role="switch"]')
  vi.mocked(confirmAction).mockResolvedValueOnce(false)
  await toggle.trigger('click'); await flushPromises()
  expect(fetchSubscriptionTemplate).not.toHaveBeenCalled()
  expect(updateSubscriptionTemplate).not.toHaveBeenCalled()
  expect(toggle.attributes('aria-checked')).toBe('true')
  vi.mocked(confirmAction).mockResolvedValueOnce(true)
  vi.mocked(fetchSubscriptionTemplate).mockResolvedValue({ ...item, revision: 9, customization: { client: {}, policy_groups: [] } } as any)
  let resolve!: (value: any) => void
  vi.mocked(updateSubscriptionTemplate).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  await toggle.trigger('click'); await flushPromises()
  expect(toggle.attributes('disabled')).toBeDefined()
  expect(toggle.attributes('aria-busy')).toBe('true')
  await toggle.trigger('click'); await flushPromises()
  expect(updateSubscriptionTemplate).toHaveBeenCalledTimes(1)
  expect(updateSubscriptionTemplate).toHaveBeenCalledWith(1, expect.objectContaining({ is_active: false, expected_revision: 9 }))
  vi.mocked(fetchSubscriptionTemplatesPage).mockResolvedValue({ items: [{ ...item, is_active: false }], total: 1 } as any)
  resolve({ ...item, is_active: false }); await flushPromises()
  expect(toggle.attributes('aria-checked')).toBe('false')
})

it('retains the active state and exposes the actual save failure', async () => {
  vi.mocked(confirmAction).mockResolvedValueOnce(true)
  vi.mocked(fetchSubscriptionTemplate).mockResolvedValue({ ...item, customization: {} } as any)
  vi.mocked(updateSubscriptionTemplate).mockRejectedValueOnce({ response: { data: { message: '模板已由其他管理员修改' } } })
  const view = await render()
  await view.get('tbody [role="switch"]').trigger('click'); await flushPromises()
  expect(view.get('tbody [role="switch"]').attributes('aria-checked')).toBe('true')
  expect(view.text()).toContain('模板已由其他管理员修改')
})
