import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountOrdersPage, fetchAccountSubscriptionsPage, fetchTrafficQuota } from '../../api/client'
import MetricCard from '../../components/MetricCard.vue'
import AccountDashboard from './AccountDashboard.vue'

vi.mock('../../api/client', () => ({
  fetchAccountOrdersPage: vi.fn(), fetchAccountSubscriptionsPage: vi.fn(), fetchTrafficQuota: vi.fn(),
}))
vi.mock('../../stores/app', () => ({ useAppStore: () => ({ user: { email: 'fixture@example.test' } }) }))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function page(items: unknown[], total = items.length) { return { items, total } as any }
describe('dashboard independent sections', () => {
  let wrapper: VueWrapper | undefined
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchTrafficQuota).mockResolvedValue({ remaining_bytes: 2048, scope_user: 1, as_of: '2026-10-16T00:00:00Z' })
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ id: 1, plan_name: 'Fixture service', end_at: '2027-01-01', flow_total: 2048 }], 20))
    vi.mocked(fetchAccountOrdersPage).mockImplementation(async params => params?.status ? page([], 7)
      : page([{ id: 2, plan_name: 'Fixture order', amount_cents: 100, currency: 'CNY', status: 'pending' }], 15))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })
  async function open() {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push('/account')
    await router.isReady()
    wrapper = mount(AccountDashboard, { global: { plugins: [router] } })
    await flushPromises()
  }
  it('renders successful sections while another summary remains pending', async () => {
    vi.mocked(fetchTrafficQuota).mockReturnValue(deferred<any>().promise)
    await open()
    expect(wrapper!.text()).toContain('Fixture service')
    expect(wrapper!.text()).toContain('Fixture order')
    const cards = wrapper!.findAllComponents(MetricCard)
    expect(cards.map(card => card.props('value'))).toEqual(['—', 20, 7])
    expect(wrapper!.text()).not.toContain('还没有订单')
  })
  it('shows the complete quota rather than summing the three visible subscriptions', async () => {
    await open()
    expect(wrapper!.findAllComponents(MetricCard)[0].props('value')).toBe('2.00 KB')
    expect(wrapper!.text()).toContain('全部有效订阅的可用流量合计')
    expect(wrapper!.text()).not.toContain('累计使用')
    expect(fetchTrafficQuota).toHaveBeenCalledWith(false, { signal: expect.any(AbortSignal) })
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith({ status: 'active', offset: 0, limit: 3 }, { signal: expect.any(AbortSignal) })
  })
  it('keeps the last confirmed quota visible during a slow refresh and after a refresh failure', async () => {
    await open()
    const slow = deferred<any>()
    vi.mocked(fetchTrafficQuota).mockReturnValueOnce(slow.promise)
    await wrapper!.findAll('button').find(button => button.text().trim().includes('刷新'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.findAllComponents(MetricCard)[0].props('value')).toBe('2.00 KB')
    expect(wrapper!.findAllComponents(MetricCard)[0].props('status')).toBe('更新中')
    slow.resolve({ remaining_bytes: 1024, scope_user: 1, as_of: '2026-10-16T00:00:01Z' })
    await flushPromises()
    vi.mocked(fetchTrafficQuota).mockRejectedValueOnce(new Error('offline'))
    await wrapper!.findAll('button').find(button => button.text().trim().includes('刷新'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.findAllComponents(MetricCard)[0].props('value')).toBe('1.00 KB')
    expect(wrapper!.findAllComponents(MetricCard)[0].props('status')).toBe('更新失败')
  })
  it('shows pending or failed reads as unknown rather than empty lists or zero counts', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValue(deferred<any>().promise)
    vi.mocked(fetchAccountOrdersPage).mockRejectedValue(new Error('offline'))
    await open()
    expect(wrapper!.text()).toContain('正在加载有效订阅')
    expect(wrapper!.text()).toContain('最近订单加载失败')
    expect(wrapper!.text()).not.toContain('还没有有效订阅')
    expect(wrapper!.text()).not.toContain('还没有订单')
    expect(wrapper!.findAllComponents(MetricCard).at(-1)!.props('value')).toBe('—')
  })
  it('retries only the failed section without re-fetching successful summaries', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockRejectedValueOnce(new Error('offline'))
    await open()
    expect(wrapper!.text()).toContain('Fixture order')
    await wrapper!.findAll('button').find(button => button.text().includes('重试订阅'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('Fixture service')
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledTimes(2)
    expect(fetchTrafficQuota).toHaveBeenCalledOnce()
    expect(fetchAccountOrdersPage).toHaveBeenCalledTimes(2)
  })
  it('renders genuine successful empty states only after each read succeeds', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([]))
    vi.mocked(fetchAccountOrdersPage).mockResolvedValue(page([]))
    await open()
    expect(wrapper!.text()).toContain('还没有有效订阅')
    expect(wrapper!.text()).toContain('还没有订单')
    expect(wrapper!.findAllComponents(MetricCard).at(-1)!.props('value')).toBe(0)
  })
  it('shows an exhausted valid subscription and binds reset to that instance', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ id: 61, status: 'active', plan_name: 'Exhausted monthly plan', end_at: '2027-01-01', flow_total: 100, flow_used: 100, reset_quota_bytes: 100, can_reset: true }]))
    await open()
    expect(wrapper!.text()).toContain('Exhausted monthly plan')
    expect(wrapper!.text()).toContain('本周期流量已用完')
    expect(wrapper!.find('a[href="/account/plans?operation=reset&subscription=61"]').exists()).toBe(true)
    expect(wrapper!.findAllComponents(MetricCard)[1].props('value')).toBe(1)
  })
  it('keeps an exhausted buy-only record without an unavailable reset entry', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ id: 61, status: 'active', plan_name: 'Buy-only plan', end_at: '2027-01-01', flow_total: 100, flow_used: 100, reset_quota_bytes: 100, can_reset: false }]))
    await open()
    expect(wrapper!.text()).toContain('Buy-only plan')
    expect(wrapper!.find('a[href*="operation=reset"]').exists()).toBe(false)
  })
  it('cancels every pending section on unmount' , async () => {
    const late = deferred<any>()
    vi.mocked(fetchTrafficQuota).mockReturnValue(late.promise)
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValue(late.promise)
    vi.mocked(fetchAccountOrdersPage).mockReturnValue(late.promise)
    await open()
    const signals = [
      vi.mocked(fetchTrafficQuota).mock.calls[0]?.[1]?.signal,
      vi.mocked(fetchAccountSubscriptionsPage).mock.calls[0]?.[1]?.signal,
      ...vi.mocked(fetchAccountOrdersPage).mock.calls.map(call => call[1]?.signal),
    ]
    wrapper!.unmount(); wrapper = undefined
    expect(signals.every(signal => signal?.aborted)).toBe(true)
    late.resolve(page([]))
    await flushPromises()
  })
})
