import { reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import AccountCleanupNotice from './AccountCleanupNotice.vue'
import { fetchAccountCleanupStatus, type AccountCleanupStatus } from '../api/client'
vi.mock('../api/client', () => ({ fetchAccountCleanupStatus: vi.fn() }))
const app = reactive({ user: { id: 22 }, isAdmin: false })
vi.mock('../stores/app', () => ({ useAppStore: () => app }))
const scheduled: AccountCleanupStatus = { enabled: true, inactive_days: 14, state: 'scheduled', eligible_at: '2026-11-15T00:00:00Z', reason: '无有效订阅' }
let wrapper: VueWrapper | undefined
beforeEach(() => { app.isAdmin = false; app.user.id = 22; vi.resetAllMocks(); vi.mocked(fetchAccountCleanupStatus).mockResolvedValue(scheduled) })
afterEach(() => wrapper?.unmount())
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<p />' } }] })
  await router.push('/account'); await router.isReady()
  wrapper = mount(AccountCleanupNotice, { global: { plugins: [router] } }); await flushPromises()
  return { view: wrapper, router }
}
it('warns a scheduled account with its eligibility date and a usable purchase link', async () => {
  const { view } = await render()
  expect(view.text()).toContain('账号待清理')
  expect(view.text()).toContain('自动删除，删除后无法恢复')
  expect(view.text()).not.toContain('规则已启用')
  expect(view.get('time').attributes('datetime')).toBe(scheduled.eligible_at)
  expect(view.get('a').attributes('href')).toBe('/account/plans')
})
it('hides a disabled rule and does not invent a deadline for protected accounts', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, enabled: false, state: 'disabled', eligible_at: null })
  const { view, router } = await render()
  expect(view.find('.page-alert').exists()).toBe(false)
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'protected', eligible_at: null, reason: '钱包有余额或冻结资金' })
  await router.push('/account/wallet'); await flushPromises()
  expect(view.find('.page-alert').exists()).toBe(false)
  expect(view.find('time').exists()).toBe(false)
})
it('explains an eligible account without presenting a past deadline as a future promise', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'eligible' })
  const { view } = await render()
  expect(view.text()).toContain('已达到自动删除条件')
  expect(view.find('time').exists()).toBe(false)
})
it('does not invent a cleanup warning when the first policy read fails', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockRejectedValueOnce(new Error('offline'))
  const { view } = await render()
  expect(view.text()).toBe('')
  window.dispatchEvent(new Event('focus')); await flushPromises()
  expect(view.get('time').attributes('datetime')).toBe(scheduled.eligible_at)
})
it('retains a known countdown and its recovery action when refresh fails', async () => {
  const { view } = await render()
  vi.mocked(fetchAccountCleanupStatus).mockRejectedValueOnce(new Error('offline'))
  window.dispatchEvent(new Event('focus')); await flushPromises()
  expect(view.text()).toContain('清理安排更新失败，显示上次结果')
  expect(view.get('time').attributes('datetime')).toBe(scheduled.eligible_at)
  const retry = view.findAll('button').find(button => button.text() === '重新读取')!
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'subscribed' })
  await retry.trigger('click'); await flushPromises()
  expect(view.text()).toBe('')
})
it('does not warn a subscribed account when a background refresh fails', async () => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'subscribed' })
  const { view } = await render()
  vi.mocked(fetchAccountCleanupStatus).mockRejectedValueOnce(new Error('offline'))
  window.dispatchEvent(new Event('focus')); await flushPromises()
  expect(view.text()).toBe('')
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

it.each([
  ['administrator protection', { state: 'protected', reason: '管理员账号受保护', eligible_at: null }],
  ['wallet protection', { state: 'protected', reason: '钱包有余额或冻结资金', eligible_at: null }],
  ['pending-order protection', { state: 'protected', reason: '存在未结订单', eligible_at: null }],
  ['active monthly subscription', { state: 'subscribed', eligible_at: scheduled.eligible_at }],
  ['long-term subscription', { state: 'subscribed', eligible_at: null }],
] as const)('does not warn an unaffected account: %s', async (_name, status) => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, ...status })
  const { view } = await render()
  expect(view.find('.page-alert').exists()).toBe(false)
  expect(view.text()).toBe('')
})
it('skips policy requests and focus refreshes for administrators', async () => {
  app.isAdmin = true
  const { view } = await render()
  window.dispatchEvent(new Event('focus')); await flushPromises()
  expect(fetchAccountCleanupStatus).not.toHaveBeenCalled()
  expect(view.text()).toBe('')
})
it('removes a stale countdown when the user buys a valid subscription', async () => {
  const { view } = await render()
  expect(view.find('time').exists()).toBe(true)
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, state: 'subscribed' })
  window.dispatchEvent(new Event('focus')); await flushPromises()
  expect(view.find('.page-alert').exists()).toBe(false)
})
it('cancels a pending policy read when the account becomes an administrator', async () => {
  let resolve!: (value: AccountCleanupStatus) => void
  vi.mocked(fetchAccountCleanupStatus).mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const { view } = await render()
  const signal = vi.mocked(fetchAccountCleanupStatus).mock.calls[0]![0]!.signal!
  app.isAdmin = true; await flushPromises()
  resolve(scheduled); await flushPromises()
  expect(signal.aborted).toBe(true)
  expect(view.text()).toBe('')
})
it.each([null, 'not-a-date'])('does not invent a deletion time for an unavailable deadline: %s', async eligible_at => {
  vi.mocked(fetchAccountCleanupStatus).mockResolvedValueOnce({ ...scheduled, eligible_at })
  const { view } = await render()
  expect(view.text()).toContain('已进入自动清理范围')
  expect(view.find('time').exists()).toBe(false)
})
