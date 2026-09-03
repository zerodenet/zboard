import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountOrdersPage, fetchAccountSubscriptionsPage, fetchTrafficSummary } from '../../api/client'
import MetricCard from '../../components/MetricCard.vue'
import AccountDashboard from './AccountDashboard.vue'

vi.mock('../../api/client', () => ({
  fetchAccountOrdersPage: vi.fn(), fetchAccountSubscriptionsPage: vi.fn(), fetchTrafficSummary: vi.fn(),
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
    vi.mocked(fetchTrafficSummary).mockResolvedValue({ remaining_bytes: 2048, total_used_bytes: 1024 })
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ id: 1, plan_name: 'Fixture service', end_at: '2027-01-01', flow_total: 2048 }], 20))
    vi.mocked(fetchAccountOrdersPage).mockImplementation(async params => params?.status ? page([], 7)
      : page([{ id: 2, plan_name: 'Fixture order', amount_cents: 100, currency: 'CNY', status: 'pending' }], 15))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })
  async function open() {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push('/account')
    await router.isReady()
    wrapper = mount(AccountDashboard, { global: { plugins: [router, PrimeVue] } })
    await flushPromises()
  }
  it('renders successful sections while another summary remains pending', async () => {
    vi.mocked(fetchTrafficSummary).mockReturnValue(deferred<any>().promise)
    await open()
    expect(wrapper!.text()).toContain('Fixture service')
    expect(wrapper!.text()).toContain('Fixture order')
    const cards = wrapper!.findAllComponents(MetricCard)
    expect(cards.map(card => card.props('value'))).toEqual(['—', 20, 7])
    expect(wrapper!.text()).not.toContain('还没有订单')
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
    expect(fetchTrafficSummary).toHaveBeenCalledOnce()
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
  it('cancels every pending section on unmount', async () => {
    const late = deferred<any>()
    vi.mocked(fetchTrafficSummary).mockReturnValue(late.promise)
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValue(late.promise)
    vi.mocked(fetchAccountOrdersPage).mockReturnValue(late.promise)
    await open()
    const signals = [
      vi.mocked(fetchTrafficSummary).mock.calls[0]?.[1]?.signal,
      vi.mocked(fetchAccountSubscriptionsPage).mock.calls[0]?.[1]?.signal,
      ...vi.mocked(fetchAccountOrdersPage).mock.calls.map(call => call[1]?.signal),
    ]
    wrapper!.unmount(); wrapper = undefined
    expect(signals.every(signal => signal?.aborted)).toBe(true)
    late.resolve(page([]))
    await flushPromises()
  })
})
