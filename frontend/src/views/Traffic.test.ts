import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountSubscriptionsPage, fetchTrafficReconciliationPage, fetchTrafficSummary } from '../api/client'
import { emptyEntityReferenceResponse, fetchAdminEntityReferences, fetchTrafficTrends } from '../api/readModels'
import { fetchTrafficNodeSeries, fetchTrafficUsagePage, fetchTrafficUsageStatistics } from '../api/trafficUsage'
import CursorPager from '../components/CursorPager.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import NodeTrafficChart from './account/NodeTrafficChart.vue'
import TrafficObservabilityChart from './account/TrafficObservabilityChart.vue'
import AccountTraffic from './account/AccountTraffic.vue'
import Traffic from './Traffic.vue'

vi.mock('../api/client', () => ({
  API_BASE: '/api/v1', getAuthToken: () => '',
  fetchTrafficReconciliationPage: vi.fn(), fetchTrafficSummary: vi.fn(),
  fetchAccountSubscriptionsPage: vi.fn(),
}))
vi.mock('../api/trafficUsage', () => ({ fetchTrafficUsagePage: vi.fn(), fetchTrafficUsageStatistics: vi.fn(), fetchTrafficNodeSeries: vi.fn() }))
vi.mock('../api/readModels', async importOriginal => ({
  ...await importOriginal<typeof import('../api/readModels')>(),
  fetchAdminEntityReferences: vi.fn(), fetchTrafficTrends: vi.fn(),
}))
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}
function records(id = 1) {
  return { items: [{ id, user_id: id, subscription_id: id, node_id: id, record_at: '2026-09-01', used_bytes: 1024 }],
    total: 200, page: { next_cursor: 'next', previous_cursor: 'previous' }, aggregates: {} } as any
}
function trend(id = 1) {
  return { points: [{ date: '2026-09-01', label: `trend-${id}`, used_bytes: id, peak_connections: id }],
    subscriptions: [], peak_connections: id, record_count: id, connection_sample_count: id, truncated: false } as any
}
function nodes(id = 1) {
  return { bucket: 'hour', points: [{ record_at: '2026-09-01', node_id: id, used_bytes: id }],
    nodes: [{ id, kind: 'node', display_name: `node-${id}` }], node_limit: 20, truncated: false } as any
}
function references(id: number) {
  return { ...emptyEntityReferenceResponse(), nodes: { [id]: { id, kind: 'node', display_name: `resolved-node-${id}` } } }
}
describe.each([false, true])('traffic independent reads (admin=%s)', admin => {
  let wrapper: VueWrapper | undefined, router: Router
  const path = admin ? '/admin/traffic' : '/account/traffic'
  const range = '?from=2026-09-01&to=2026-09-02'
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchTrafficSummary).mockResolvedValue({})
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue({ items: [], total: 0 } as any)
    vi.mocked(fetchTrafficUsageStatistics).mockResolvedValue({ total: 200, aggregates: {}, as_of: '2026-09-01T00:00:00Z' } as any)
    vi.mocked(fetchTrafficReconciliationPage).mockResolvedValue({ items: [], total: 0, aggregates: {} } as any)
    vi.mocked(fetchTrafficUsagePage).mockResolvedValue(records())
    vi.mocked(fetchTrafficTrends).mockResolvedValue(trend())
    vi.mocked(fetchTrafficNodeSeries).mockResolvedValue(nodes())
    vi.mocked(fetchAdminEntityReferences).mockResolvedValue(references(1))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })
  async function open() {
    router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push(path + range)
    await router.isReady()
    wrapper = mount(admin ? Traffic : AccountTraffic, { global: {
      plugins: [router, PrimeVue], stubs: { NodeTrafficChart: true, TrafficObservabilityChart: true },
    } })
    await flushPromises()
  }
  it('starts the independent node chart without waiting for records or trends', async () => {
    vi.mocked(fetchTrafficUsagePage).mockReturnValue(deferred<any>().promise)
    vi.mocked(fetchTrafficTrends).mockReturnValue(deferred<any>().promise)
    await open()
    expect(fetchTrafficNodeSeries).toHaveBeenCalledOnce()
    expect(wrapper!.getComponent(NodeTrafficChart).props('data')!.nodes[0]!.id).toBe(1)
    expect(wrapper!.text()).toContain('正在加载流量记录')
    expect(wrapper!.text()).not.toContain('没有流量使用明细')
  })
  it('keeps the record pager usable while charts or reference names are slow', async () => {
    vi.mocked(fetchTrafficTrends).mockReturnValue(deferred<any>().promise)
    vi.mocked(fetchTrafficNodeSeries).mockReturnValue(deferred<any>().promise)
    vi.mocked(fetchAdminEntityReferences).mockReturnValue(deferred<any>().promise)
    await open()
    expect(wrapper!.getComponent(CursorPager).props('loading')).toBe(false)
    expect(wrapper!.findAllComponents(DataWorkbench)[0]!.props('loading')).toBe(false)
  })
  it('isolates chart failures from records and retries only the failed chart', async () => {
    vi.mocked(fetchTrafficNodeSeries).mockRejectedValueOnce(new Error('offline'))
    await open()
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(1)
    expect(wrapper!.findComponent(NodeTrafficChart).exists()).toBe(false)
    await wrapper!.findAll('button').find(button => button.text().includes('重试节点流量'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.getComponent(NodeTrafficChart).props('data')!.nodes[0]!.id).toBe(1)
    expect(fetchTrafficNodeSeries).toHaveBeenCalledTimes(2)
    expect(fetchTrafficTrends).toHaveBeenCalledOnce()
    expect(fetchTrafficUsagePage).toHaveBeenCalledOnce()
  })
  it('does not rerun whole-range charts when cursor navigation is restored from the URL', async () => {
    await open()
    await router.push(path + range + '&cursor=next')
    await flushPromises()
    expect(fetchTrafficUsagePage).toHaveBeenCalledTimes(2)
    expect(fetchTrafficUsageStatistics).toHaveBeenCalledOnce()
    expect(fetchTrafficTrends).toHaveBeenCalledOnce()
    expect(fetchTrafficNodeSeries).toHaveBeenCalledOnce()
    if (admin) expect(fetchTrafficReconciliationPage).toHaveBeenCalledOnce()
    else expect(fetchTrafficSummary).toHaveBeenCalledOnce()
  })
  it('clears old-filter charts immediately and ignores late successes and failures', async () => {
    const oldTrend = deferred<any>(), oldNodes = deferred<any>()
    vi.mocked(fetchTrafficTrends).mockReturnValueOnce(oldTrend.promise).mockResolvedValueOnce(trend(2))
    vi.mocked(fetchTrafficNodeSeries).mockReturnValueOnce(oldNodes.promise).mockResolvedValueOnce(nodes(2))
    await open()
    await router.push(path + range + '&subscription_id=2')
    await flushPromises()
    oldTrend.resolve(trend(1)); oldNodes.reject(new Error('old failure'))
    await flushPromises()
    expect(wrapper!.getComponent(TrafficObservabilityChart).props('peakConnections')).toBe(2)
    expect(wrapper!.getComponent(NodeTrafficChart).props('data')!.nodes[0]!.id).toBe(2)
    expect(wrapper!.text()).not.toContain('old failure')
    expect(vi.mocked(fetchTrafficTrends).mock.calls[0]![0].signal?.aborted).toBe(true)
  })
  it('coarsens long node-chart windows without changing the record table bucket', async () => {
    await open()
    await router.push(path + '?from=2026-01-01&to=2026-02-01&bucket=minute')
    await flushPromises()
    expect(fetchTrafficNodeSeries).toHaveBeenLastCalledWith(expect.objectContaining({ bucket: 'day' }), admin, expect.anything())
    expect(fetchTrafficUsagePage).toHaveBeenLastCalledWith(expect.objectContaining({ bucket: 'minute' }), admin, expect.anything())
  })
  it('does not show failed records as a successful empty history', async () => {
    vi.mocked(fetchTrafficUsagePage).mockRejectedValue(new Error('offline'))
    await open()
    expect(wrapper!.text()).toContain('流量记录加载失败')
    expect(wrapper!.text()).not.toContain('没有流量使用明细')
    expect(wrapper!.findComponent(NodeTrafficChart).exists()).toBe(true)
  })
  it('keeps the pager usable with unknown totals while the range statistics are pending', async () => {
    vi.mocked(fetchTrafficUsageStatistics).mockReturnValue(deferred<any>().promise)
    await open()
    expect(wrapper!.getComponent(CursorPager).props('total')).toBeUndefined()
    expect(wrapper!.getComponent(CursorPager).props('loading')).toBe(false)
    expect(wrapper!.text()).toContain('正在计算流量区间统计')
    expect(fetchTrafficUsagePage).toHaveBeenCalledWith(expect.objectContaining({ includeTotals: false }), admin, expect.anything())
    await router.push(path + range + '&cursor=next')
    await flushPromises()
    expect(fetchTrafficUsageStatistics).toHaveBeenCalledOnce()
    expect(fetchTrafficUsagePage).toHaveBeenCalledTimes(2)
  })
  it('retries failed statistics without reloading records and identifies snapshot freshness', async () => {
    vi.mocked(fetchTrafficUsageStatistics).mockRejectedValueOnce(new Error('offline'))
    await open()
    expect(wrapper!.text()).toContain('流量区间统计加载失败')
    expect(wrapper!.getComponent(CursorPager).props('total')).toBeUndefined()
    await wrapper!.findAll('button').find(button => button.text().includes('重试区间统计'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.getComponent(CursorPager).props('total')).toBe(200)
    expect(wrapper!.text()).toContain('翻页不刷新统计')
    expect(fetchTrafficUsagePage).toHaveBeenCalledOnce()
    expect(fetchTrafficUsageStatistics).toHaveBeenCalledTimes(2)
  })
  it('clears old-scope statistics and ignores late responses after a filter change', async () => {
    const late = deferred<any>()
    vi.mocked(fetchTrafficUsageStatistics).mockReturnValueOnce(late.promise)
      .mockResolvedValueOnce({ total: 9, aggregates: {}, as_of: '2026-09-02T00:00:00Z' } as any)
    await open()
    await router.push(path + range + '&subscription_id=2')
    await flushPromises()
    late.resolve({ total: 1234, aggregates: {}, as_of: '2026-09-01T00:00:00Z' })
    await flushPromises()
    expect(wrapper!.getComponent(CursorPager).props('total')).toBe(9)
    expect(vi.mocked(fetchTrafficUsageStatistics).mock.calls[0]![2]?.signal?.aborted).toBe(true)
  })
  it('aborts every pending request on unmount and prevents further dependent reads', async () => {
    const late = deferred<any>()
    vi.mocked(fetchTrafficUsagePage).mockReturnValue(late.promise)
    vi.mocked(fetchTrafficTrends).mockReturnValue(late.promise)
    vi.mocked(fetchTrafficNodeSeries).mockReturnValue(late.promise)
    vi.mocked(fetchTrafficUsageStatistics).mockReturnValue(late.promise)
    await open()
    const signals = [
      vi.mocked(fetchTrafficUsagePage).mock.calls[0]?.[2]?.signal,
      vi.mocked(fetchTrafficTrends).mock.calls[0]?.[0].signal,
      vi.mocked(fetchTrafficNodeSeries).mock.calls[0]?.[2]?.signal,
      vi.mocked(fetchTrafficUsageStatistics).mock.calls[0]?.[2]?.signal,
    ]
    wrapper!.unmount(); wrapper = undefined
    expect(signals.every(signal => signal?.aborted)).toBe(true)
    late.resolve(records())
    await flushPromises()
    expect(fetchAdminEntityReferences).not.toHaveBeenCalled()
  })
  if (!admin) {
    it('renders page-owned references outside chart rankings while charts are still pending', async () => {
      vi.mocked(fetchTrafficTrends).mockReturnValue(deferred<any>().promise)
      vi.mocked(fetchTrafficNodeSeries).mockReturnValue(deferred<any>().promise)
      vi.mocked(fetchTrafficUsagePage).mockResolvedValue({ ...records(99), facets: {
        nodes: { 99: { id: 99, kind: 'node', display_name: 'Page-only node' } },
        subscriptions: { 99: { id: 99, kind: 'subscription', display_name: 'Older subscription' } },
      } })
      await open()
      expect(wrapper!.text()).toContain('Page-only node')
      expect(wrapper!.text()).toContain('Older subscription')
      expect(fetchAdminEntityReferences).not.toHaveBeenCalled()
      expect(wrapper!.getComponent(CursorPager).props('loading')).toBe(false)
    })
    it('replaces record references with their page and ignores superseded page responses', async () => {
      const late = deferred<any>()
      vi.mocked(fetchTrafficUsagePage).mockReturnValueOnce(late.promise)
      await open()
      vi.mocked(fetchTrafficUsagePage).mockResolvedValue({ ...records(2), facets: {
        nodes: { 2: { id: 2, kind: 'node', display_name: 'Current page node' } }, subscriptions: {},
      } })
      await router.push(path + range + '&cursor=next')
      await flushPromises()
      late.resolve({ ...records(1), facets: { nodes: { 1: { id: 1, kind: 'node', display_name: 'Stale page node' } } } })
      await flushPromises()
      expect(wrapper!.text()).toContain('Current page node')
      expect(wrapper!.text()).not.toContain('Stale page node')
      expect(wrapper!.text()).not.toContain('node-1')
    })
  }
  if (admin) {
    it('resolves record references before a slow reconciliation finishes', async () => {
      vi.mocked(fetchTrafficReconciliationPage).mockReturnValue(deferred<any>().promise)
      await open()
      expect(wrapper!.text()).toContain('resolved-node-1')
      expect(fetchAdminEntityReferences).toHaveBeenCalledWith(expect.objectContaining({ nodeIds: [1], signal: expect.any(AbortSignal) }))
      expect(wrapper!.text()).toContain('正在加载对账结果')
      expect(wrapper!.text()).not.toContain('当前没有对账异常')
    })
    it('does not let an old reference response erase current page labels', async () => {
      const old = deferred<any>()
      vi.mocked(fetchAdminEntityReferences).mockReturnValueOnce(old.promise).mockResolvedValueOnce(references(2))
      await open()
      vi.mocked(fetchTrafficUsagePage).mockResolvedValue(records(2))
      await router.push(path + range + '&cursor=next')
      await flushPromises()
      old.resolve(references(1))
      await flushPromises()
      expect(wrapper!.text()).toContain('resolved-node-2')
      expect(wrapper!.text()).not.toContain('resolved-node-1')
    })
    it('refreshes only reconciliation on its page or mode change', async () => {
      await open()
      await router.push(path + range + '&reconciliation=all')
      await flushPromises()
      expect(fetchTrafficReconciliationPage).toHaveBeenCalledTimes(2)
      expect(fetchTrafficUsagePage).toHaveBeenCalledOnce()
      expect(fetchTrafficTrends).toHaveBeenCalledOnce()
      expect(fetchTrafficNodeSeries).toHaveBeenCalledOnce()
    })
  }
})
