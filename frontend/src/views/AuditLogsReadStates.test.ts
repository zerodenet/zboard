import { shallowMount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import AuditLogs from './AuditLogs.vue'
const api = vi.hoisted(() => ({ list: vi.fn(), references: vi.fn() }))
vi.mock('../api/client', async original => ({ ...await original<typeof import('../api/client')>(), fetchAuditLogs: api.list }))
vi.mock('../api/readModels', async original => ({ ...await original<typeof import('../api/readModels')>(), fetchAdminEntityReferences: api.references }))
let view: VueWrapper | undefined
const page = (items: unknown[] = []) => ({ items, total: items.length, page: { next_cursor: null, previous_cursor: null } })
afterEach(() => { view?.unmount(); vi.resetAllMocks() })
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/audit-logs', component: AuditLogs }] })
  await router.push('/admin/audit-logs'); await router.isReady()
  view = shallowMount(AuditLogs, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: { PageHeader: { template: '<header><slot name="actions" /></header>' }, PageRefreshButton: { name: 'PageRefreshButton', emits: ['click'], template: `<button @click="$emit('click')">刷新</button>` } } } })
  await flushPromises(); return view
}
it('does not request names for an empty audit result', async () => {
  api.list.mockResolvedValue(page())
  const w = await render()
  expect(w.findComponent({ name: 'EmptyState' }).exists()).toBe(true)
  expect(api.references).not.toHaveBeenCalled()
})
it('does not present a failed initial read as an empty audit history', async () => {
  api.list.mockRejectedValue(new Error('offline'))
  const w = await render()
  expect(w.findComponent({ name: 'TransientFeedback' }).props('error')).toBeTruthy()
  expect(w.findComponent({ name: 'EmptyState' }).exists()).toBe(false)
})
it('retains the confirmed rows when a refresh fails', async () => {
  api.list.mockResolvedValueOnce(page([{ id: 1, actor: 'admin@example.test', action: 'order.pay', target: 'order:38', created_at: '2026-11-05T08:00:00Z', has_detail: true }]))
  api.references.mockResolvedValue({ users: {}, targets: {} })
  const w = await render()
  api.list.mockRejectedValueOnce(new Error('offline'))
  await w.findComponent({ name: 'PageRefreshButton' }).trigger('click'); await flushPromises()
  expect(w.findComponent({ name: 'DataTable' }).exists()).toBe(true)
  expect(w.findComponent({ name: 'EmptyState' }).exists()).toBe(false)
  expect(w.findComponent({ name: 'TransientFeedback' }).props('error')).toBeTruthy()
})
