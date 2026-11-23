import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { afterEach, expect, it, vi } from 'vitest'
import ManagedDNS from './ManagedDNS.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), remove: vi.fn() }))
vi.mock('../api/client', () => ({
  createManagedDNSRecord: vi.fn(),
  deleteManagedDNSRecord: api.remove,
  fetchManagedDNSRecordsPage: api.list,
  fetchNodeAddressCandidates: vi.fn(),
  fetchNodesPage: vi.fn(),
  fetchProviderAccounts: vi.fn(async () => []),
  syncManagedDNSRecord: vi.fn(),
  updateManagedDNSRecord: vi.fn(),
}))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 1 }, isAdmin: true }) }))
vi.mock('../composables/useReauthenticationDraft', () => ({
  useReauthenticationDraft: () => ({ message: { value: '' }, restore: vi.fn() }),
}))

let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => wrapper?.unmount())

it('keeps a blocked DNS deletion in the confirmation dialog and retries the same record', async () => {
  const record = {
    id: 5, node_id: 1, node_name: 'Node', provider_account_id: 2, provider_name: 'Provider',
    domain_name: 'edge.example.test', record_type: 'AAAA', record_value: '2001:db8::1',
    ttl: 1, status: 'active', public_resolved: true, last_error: '',
  }
  api.list.mockResolvedValue({ items: [record], total: 1 })
  api.remove.mockRejectedValueOnce({
    response: { status: 409, data: { message: '请先完成或核验资源任务。', data: { blockers: { node_retirement: 1 } } } },
  }).mockResolvedValueOnce({ id: record.id, deleted: true, remote_record_deleted: false })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/admin/dns-records', component: ManagedDNS },
      ...['/admin/nodes', '/admin/providers', '/admin/certificates'].map(path => ({ path, component: { template: '<div />' } })),
    ],
  })
  await router.push('/admin/dns-records')
  wrapper = mount(RouterView, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  await wrapper.get('[data-row-action-trigger="dns-5"]').trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  const button = (selector: string, label: string) => {
    const match = [...document.querySelectorAll<HTMLButtonElement>(selector)].find(item => item.textContent?.trim() === label)
    expect(match).toBeDefined()
    return match!
  }
  button('[role="menu"] button', '删除').click()
  await flushPromises()
  button('[role="dialog"] button', '确认删除').click()
  await flushPromises()
  const dialog = document.querySelector('[role="dialog"]')
  expect(dialog?.textContent).toContain('这条记录关联着尚未完成或待核验的节点删除任务。')
  expect(dialog?.querySelector('a')?.getAttribute('href')).toContain('deletion=1')
  expect(wrapper.find('.transient-page-error').exists()).toBe(false)
  expect(api.remove.mock.calls).toEqual([[record.id]])

  api.list.mockResolvedValue({ items: [], total: 0 })
  button('[role="dialog"] button', '重试删除').click()
  await flushPromises()
  expect(api.remove.mock.calls).toEqual([[record.id], [record.id]])
  expect(document.querySelector('[role="dialog"]')).toBeNull()
  expect(wrapper.text()).not.toContain(record.domain_name)
})
