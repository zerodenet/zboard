import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import SubscriptionCredentialMigration from './SubscriptionCredentialMigration.vue'
import { fetchSubscriptionCredentialMigration, prepareSubscriptionCredentialMigration, rollbackSubscriptionCredentialMigration } from '../api/client'

vi.mock('../api/client', () => ({ fetchSubscriptionCredentialMigration: vi.fn(), prepareSubscriptionCredentialMigration: vi.fn(), rollbackSubscriptionCredentialMigration: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ systemTimeZone: 'Asia/Shanghai' }) }))
const initial = { subscription_id: 7, state: 'legacy', legacy_credentials: 2, pending_confirmation: 0, compatible_credentials: 0, revoking_credentials: 0, retired_credentials: 0, can_rollback: false } as const
const pending = { ...initial, state: 'awaiting_confirmation', pending_confirmation: 2, can_rollback: true } as const
beforeEach(() => { vi.clearAllMocks(); vi.mocked(fetchSubscriptionCredentialMigration).mockResolvedValue(initial); vi.mocked(prepareSubscriptionCredentialMigration).mockResolvedValue(pending) })
async function open() {
  const view = mount(SubscriptionCredentialMigration, { props: { subscriptionId: 7, active: true } })
  expect(fetchSubscriptionCredentialMigration).not.toHaveBeenCalled()
  const details = view.get('details'); (details.element as HTMLDetailsElement).open = true; await details.trigger('toggle'); await flushPromises()
  return view
}
async function click(view: Awaited<ReturnType<typeof open>>, text: string) {
  const button = view.findAll('button').find(b => b.text() === text); expect(button).toBeDefined(); await button!.trigger('click'); await flushPromises()
}
it('loads on expansion and keeps migration pending until the server confirms it', async () => {
  const view = await open(); await click(view, '合并连接凭证')
  expect(prepareSubscriptionCredentialMigration).toHaveBeenCalledTimes(1); expect(prepareSubscriptionCredentialMigration).toHaveBeenLastCalledWith(7)
  expect(view.text()).toContain('2 个旧凭证正在等待节点确认')
  expect(view.emitted('changed')).toHaveLength(1)
  view.unmount()
})
it('refreshes an uncertain outcome without replaying the mutation', async () => {
  vi.mocked(prepareSubscriptionCredentialMigration).mockRejectedValueOnce(new Error('network'))
  const view = await open(); await click(view, '合并连接凭证')
  expect(view.text()).toContain('操作结果暂未确认')
  vi.mocked(fetchSubscriptionCredentialMigration).mockResolvedValueOnce(pending)
  await click(view, '刷新')
  expect(prepareSubscriptionCredentialMigration).toHaveBeenCalledTimes(1)
  expect(view.text()).toContain('等待节点确认')
  view.unmount()
})
it('discards mutation responses from a different subscription and blocks a repeated click', async () => {
  let resolve!: (value: any) => void
  vi.mocked(prepareSubscriptionCredentialMigration).mockReturnValueOnce(new Promise(done => { resolve = done }))
  const view = await open()
  const button = view.findAll('button').find(b => b.text() === '合并连接凭证')!
  await button.trigger('click'); await button.trigger('click')
  expect(prepareSubscriptionCredentialMigration).toHaveBeenCalledTimes(1)
  vi.mocked(fetchSubscriptionCredentialMigration).mockResolvedValue({ ...initial, subscription_id: 8 })
  await view.setProps({ subscriptionId: 8 }); await flushPromises()
  resolve(pending); await flushPromises()
  expect(view.text()).toContain('按接入点维护'); expect(view.emitted('changed')).toBeUndefined()
  view.unmount()
})
it('exposes rollback only when the server says old material is recoverable', async () => {
  vi.mocked(fetchSubscriptionCredentialMigration).mockResolvedValueOnce(pending)
  vi.mocked(rollbackSubscriptionCredentialMigration).mockResolvedValueOnce(initial)
  const view = await open(); await click(view, '恢复旧凭证')
  expect(rollbackSubscriptionCredentialMigration).toHaveBeenCalledTimes(1); expect(rollbackSubscriptionCredentialMigration).toHaveBeenLastCalledWith(7)
  expect(view.text()).toContain('按接入点维护'); view.unmount()
})
