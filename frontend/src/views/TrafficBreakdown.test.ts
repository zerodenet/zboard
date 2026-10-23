import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Traffic from './Traffic.vue'
import TrafficBreakdown from './TrafficBreakdown.vue'
import UiSelect from '../components/UiSelect.vue'
import { fetchTrafficBreakdown, fetchTrafficUsagePage, fetchTrafficUsageStatistics, fetchTrafficNodeSeries } from '../api/trafficUsage'
import { fetchTrafficReconciliationPage } from '../api/client'
import { emptyEntityReferenceResponse, fetchAdminEntityReferences, fetchTrafficTrends } from '../api/readModels'

vi.mock('../api/client', () => ({ API_BASE: '/api/v1', getAuthToken: () => '', fetchTrafficReconciliationPage: vi.fn() }))
vi.mock('../api/trafficUsage', () => ({ fetchTrafficBreakdown: vi.fn(), fetchTrafficUsagePage: vi.fn(), fetchTrafficUsageStatistics: vi.fn(), fetchTrafficNodeSeries: vi.fn() }))
vi.mock('../api/readModels', async importOriginal => ({ ...await importOriginal<typeof import('../api/readModels')>(), fetchAdminEntityReferences: vi.fn(), fetchTrafficTrends: vi.fn() }))
const counters = { raw_bytes: 100, used_bytes: 200, upload_bytes: 30, download_bytes: 70, record_count: 5 }
function snapshot(dimension = 'user', name = '用户一') {
  return { dimension, items: [{ id: 1, name, missing: false, ...counters }], total: 1, totals: counters, as_of: '2026-10-16T01:00:00Z', from: '', to: '', offset: 0, limit: 25 } as any
}
describe('traffic dimension overview', () => {
  let wrapper: VueWrapper | undefined, router: Router
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchTrafficBreakdown).mockImplementation(async params => snapshot(params.dimension, params.dimension === 'user' ? '用户一' : '节点一'))
    vi.mocked(fetchTrafficTrends).mockResolvedValue({ points: [], subscriptions: [], record_count: 0, truncated: false } as any)
    vi.mocked(fetchTrafficNodeSeries).mockResolvedValue({ points: [], nodes: [], node_limit: 8, truncated: false } as any)
    vi.mocked(fetchTrafficUsagePage).mockResolvedValue({ items: [], page: {}, facets: {} } as any)
    vi.mocked(fetchTrafficUsageStatistics).mockResolvedValue({ total: 0, aggregates: {} } as any)
    vi.mocked(fetchTrafficReconciliationPage).mockResolvedValue({ items: [], total: 0, aggregates: {} } as any)
    vi.mocked(fetchAdminEntityReferences).mockResolvedValue(emptyEntityReferenceResponse())
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks() })
  async function open() {
    router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push('/admin/traffic?from=2026-10-01&to=2026-10-16')
    await router.isReady()
    wrapper = mount(Traffic, { global: { plugins: [router, createPinia()], stubs: { NodeTrafficChart: true, TrafficObservabilityChart: true } } })
    await flushPromises()
  }
  it('opens summary statistics without loading details or whole-ledger reconciliation', async () => {
    await open()
    expect(wrapper!.text()).toContain('用户一')
    expect(fetchTrafficBreakdown).toHaveBeenCalledOnce()
    expect(fetchTrafficUsagePage).not.toHaveBeenCalled()
    expect(fetchTrafficUsageStatistics).not.toHaveBeenCalled()
    expect(fetchTrafficReconciliationPage).not.toHaveBeenCalled()
  })
  it('changes dimensions, filters a selected object and explicitly loads its details', async () => {
    await open()
    const breakdown = wrapper!.getComponent(TrafficBreakdown)
    breakdown.getComponent(UiSelect).vm.$emit('update:modelValue', 'node')
    await flushPromises()
    expect(fetchTrafficBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({ dimension: 'node', offset: 0 }), true, expect.anything())
    expect(router.currentRoute.value.query.dimension).toBe('node')
    await breakdown.findAll('button').find(button => button.text() === '筛选统计')!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.node_id).toBe('1')
    expect(fetchTrafficUsagePage).not.toHaveBeenCalled()
    await wrapper!.getComponent(TrafficBreakdown).findAll('button').find(button => button.text() === '用量明细')!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.section).toBe('details')
    expect(fetchTrafficUsagePage).toHaveBeenCalledWith(expect.objectContaining({ nodeId: '1', from: '2026-10-01', to: '2026-10-16' }), true, expect.anything())
    expect(fetchTrafficReconciliationPage).toHaveBeenCalledOnce()
    router.back(); await flushPromises()
    expect(wrapper!.getComponent(TrafficBreakdown).props('dimension')).toBe('node')
    expect(fetchTrafficUsagePage).toHaveBeenCalledOnce()
  })
  it('displays load failures as errors and retries only the dimension query', async () => {
    vi.mocked(fetchTrafficBreakdown).mockRejectedValueOnce(new Error('offline'))
    await open()
    expect(wrapper!.text()).toContain('维度统计加载失败')
    expect(wrapper!.text()).not.toContain('没有维度统计')
    await wrapper!.findAll('button').find(button => button.text() === '重试统计')!.trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('用户一')
    expect(fetchTrafficNodeSeries).toHaveBeenCalledOnce()
    expect(fetchTrafficUsagePage).not.toHaveBeenCalled()
  })
  it('cancels stale dimension requests and ignores their late responses', async () => {
    let resolve!: (value: any) => void
    vi.mocked(fetchTrafficBreakdown).mockReturnValueOnce(new Promise(done => { resolve = done }))
    await open()
    wrapper!.getComponent(TrafficBreakdown).getComponent(UiSelect).vm.$emit('update:modelValue', 'node')
    await flushPromises()
    resolve(snapshot('user', '过时用户'))
    await flushPromises()
    expect(wrapper!.text()).toContain('节点一')
    expect(wrapper!.text()).not.toContain('过时用户')
    expect(vi.mocked(fetchTrafficBreakdown).mock.calls[0]![2]!.signal!.aborted).toBe(true)
  })
  it('refreshes every three seconds only while visible and stops after unmount', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    await open()
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    expect(fetchTrafficBreakdown).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(6000)
    expect(fetchTrafficBreakdown).toHaveBeenCalledTimes(2)
    wrapper!.unmount(); wrapper = undefined
    visibility.mockReturnValue('visible')
    await vi.advanceTimersByTimeAsync(6000)
    expect(fetchTrafficBreakdown).toHaveBeenCalledTimes(2)
  })
})
