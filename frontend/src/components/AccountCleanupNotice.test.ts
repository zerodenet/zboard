import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import AccountCleanupNotice from './AccountCleanupNotice.vue'
import { fetchAccountCleanupStatus, type AccountCleanupStatus } from '../api/client'
vi.mock('../api/client', () => ({ fetchAccountCleanupStatus: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ user: { id: 22 } }) }))
const scheduled: AccountCleanupStatus = { enabled: true, inactive_days: 14, state: 'scheduled', eligible_at: '2026-11-15T00:00:00Z', reason: '无有效订阅' }
let wrapper: VueWrapper | undefined
beforeEach(() => { vi.resetAllMocks(); vi.mocked(fetchAccountCleanupStatus).mockResolvedValue(scheduled) })
afterEach(() => wrapper?.unmount())
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<p />' } }] })
  await router.push('/account'); await router.isReady()
  wrapper = mount(AccountCleanupNotice, { global: { plugins: [router] } }); await flushPromises()
  return { view: wrapper, router }
}
it('shows the enabled rule and the exact eligibility date with a usable purchase link', async () => {
  const { view } = await render()
  expect(view.text()).toContain('连续 14 天无有效订阅')
  expect(view.text()).toContain('永久删除，无法恢复')
  expect(view.get('time').attributes('datetime')).toBe(scheduled.eligible_at)
  expect(view.get('a').attributes('href')).toBe('/account/plans')
})
it('hides a disabled rule and does not invent a deadline for protected accounts', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, enabled: false, state: 'disabled', eligible_at: null })
  const { view, router } = await render()
  expect(view.find('.page-alert').exists()).toBe(false)
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'protected', eligible_at: null, reason: '钱包有余额或冻结资金' })
  await router.push('/account/wallet'); await flushPromises()
  expect(view.text()).toContain('当前账号暂不删除：钱包有余额或冻结资金')
  expect(view.find('time').exists()).toBe(false)
})
it('explains an eligible account without presenting a past deadline as a future promise', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'eligible' })
  const { view } = await render()
  expect(view.text()).toContain('等待自动删除')
  expect(view.find('time').exists()).toBe(false)
})
it('keeps failures visible and retries the policy read', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockRejectedValueOnce(new Error('offline'))
  const { view } = await render()
  expect(view.text()).toContain('清理规则暂时无法读取')
  await view.get('button').trigger('click'); await flushPromises()
  expect(view.get('time').attributes('datetime')).toBe(scheduled.eligible_at)
})
it('aborts a previous page read and cannot display its stale rule after navigation', async () => {
  let resolve!: (value: AccountCleanupStatus) => void
  vi.mocked(fetchAccountCleanupStatus).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const { view, router } = await render()
  const signal = vi.mocked(fetchAccountCleanupStatus).mock.calls[0]![0]!.signal!
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, enabled: false, state: 'disabled' })
  await router.push('/account/subscription'); await flushPromises()
  resolve(scheduled); await flushPromises()
  expect(signal.aborted).toBe(true)
  expect(view.find('.page-alert').exists()).toBe(false)
})

it('refreshes the rule on return to the window without starting a polling loop', async () => {
  const { view } = await render()
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, enabled: false, state: 'disabled' })
  window.dispatchEvent(new Event('focus')); await flushPromises()
  expect(fetchAccountCleanupStatus).toHaveBeenCalledTimes(2)
  expect(view.find('.page-alert').exists()).toBe(false)
})
