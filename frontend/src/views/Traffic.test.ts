import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createPinia } from 'pinia'
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
import { useAppStore } from '../stores/app'
import { setDisplayTimeZone } from '../utils/timeZone'
import TimeBadge from '../components/TimeBadge.vue'
import type { TrafficTrendScope } from '../api/readModels'

vi.mock('../api/client', () => ({
  API_BASE: '/api/v1', getAuthToken: () => '',
  fetchTrafficReconciliationPage: vi.fn(), fetchTrafficSummary: vi.fn(),
  fetchAccountSubscriptionsPage: vi.fn(),
}))
vi.mock('../api/trafficUsage', () => ({ fetchTrafficUsagePage: vi.fn(), fetchTrafficUsageStatistics: vi.fn(), fetchTrafficNodeSeries: vi.fn(), fetchTrafficBreakdown: vi.fn() }))
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
function trend(id = 1, scope?: TrafficTrendScope) {
  return { from: '2026-09-01', to: '2026-09-02', projection_pending: false, scope,
    points: [{ date: '2026-09-01', label: `trend-${id}`, used_bytes: id, upload_bytes: id * 2, download_bytes: id * 3, record_count: id, peak_connections: id }],
    subscriptions: [], peak_connections: id, record_count: id, connection_sample_count: id, truncated: false,
    as_of: id === 1 ? '2026-09-01T01:00:00Z' : '2026-09-02T01:00:00Z' } as any
}
function pending(scope: TrafficTrendScope) {
  return { from: '2026-09-01', to: '2026-09-02', projection_pending: true, scope,
    points: null, subscriptions: null, peak_connections: null, record_count: null,
    connection_sample_count: null, truncated: null, as_of: null } as const
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
  let app: ReturnType<typeof useAppStore>
  const path = admin ? '/admin/traffic' : '/account/traffic'
  const range = '?from=2026-09-01&to=2026-09-02' + (admin ? '&section=details' : '')
  const scope = (extra: Partial<TrafficTrendScope> = {}): TrafficTrendScope => ({ administrative: admin, user_id: admin ? 0 : 1, subscription_id: 0, node_id: 0, protocol_endpoint_id: 0, time_zone: 'UTC', ...extra })
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchTrafficSummary).mockResolvedValue({})
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue({ items: [], total: 0 } as any)
    vi.mocked(fetchTrafficUsageStatistics).mockResolvedValue({ total: 200, aggregates: {}, as_of: '2026-09-01T00:00:00Z' } as any)
    vi.mocked(fetchTrafficReconciliationPage).mockResolvedValue({ items: [], total: 0, aggregates: {} } as any)
    vi.mocked(fetchTrafficUsagePage).mockResolvedValue(records())
    vi.mocked(fetchTrafficTrends).mockResolvedValue(trend(1, scope()))
    vi.mocked(fetchTrafficNodeSeries).mockResolvedValue(nodes())
    vi.mocked(fetchAdminEntityReferences).mockResolvedValue(references(1))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; setDisplayTimeZone('UTC') })
  async function open(realChart = false) {
    router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push(path + range)
    await router.isReady()
    const pinia = createPinia()
    app = useAppStore(pinia)
    app.$patch({ user: { id: 1, email: 'fixture@example.test', isAdmin: admin } })
    wrapper = mount(admin ? Traffic : AccountTraffic, { global: {
      plugins: [router, pinia], stubs: { NodeTrafficChart: true, TrafficObservabilityChart: !realChart },
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
  it('shows first pending statistics as unknown rather than zero or no activity', async () => {
    vi.mocked(fetchTrafficTrends).mockResolvedValue(pending(scope()))
    await open(true)
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    expect(chart.text()).toContain('统计更新中')
    expect(chart.text()).toContain('当前范围统计未知')
    expect(chart.text()).not.toContain('当前范围没有可绘制的数据')
    expect(chart.text()).not.toContain('暂无连接数据')
    expect(chart.find('.traffic-chart, .connection-chart').exists()).toBe(false)
    expect(chart.findComponent(TimeBadge).exists()).toBe(false)
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(1)
  })
  it('retains the same-scope complete curve and its time while pending, then replaces it on refresh', async () => {
    vi.mocked(fetchTrafficTrends).mockResolvedValueOnce(trend(1, scope()))
      .mockResolvedValueOnce(pending(scope())).mockResolvedValueOnce(trend(2, scope()))
    await open(true)
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    const oldCurve = chart.get('.traffic-line.used').attributes('d')
    const refresh = () => chart.findAll('button').find(button => button.text() === '刷新趋势')!.trigger('click')
    await refresh(); await flushPromises()
    expect(chart.text()).toContain('上次完整统计')
    expect(chart.get('.traffic-line.used').attributes('d')).toBe(oldCurve)
    expect(chart.getComponent(TimeBadge).props('value')).toBe('2026-09-01T01:00:00Z')
    expect(fetchTrafficTrends).toHaveBeenCalledTimes(2)
    expect(fetchTrafficUsagePage).toHaveBeenCalledOnce()
    await refresh(); await flushPromises()
    expect(chart.text()).not.toContain('统计更新中')
    expect(chart.props('points')[0].used_bytes).toBe(2)
    expect(chart.getComponent(TimeBadge).props('value')).toBe('2026-09-02T01:00:00Z')
    expect(fetchTrafficUsagePage).toHaveBeenCalledOnce()
  })
  it.each(admin ? ['subscription_id', 'user_id', 'node_id', 'protocol_endpoint_id'] as const : ['subscription_id'] as const)('clears a complete curve on %s change before accepting pending statistics in the new scope', async field => {
    vi.mocked(fetchTrafficTrends).mockResolvedValueOnce(trend(1, scope()))
      .mockResolvedValueOnce(pending(scope({ [field]: 2 })))
    await open(true)
    await router.push(path + range + `&${field}=2`); await flushPromises()
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    expect(chart.text()).toContain('当前范围统计未知')
    expect(chart.find('.traffic-chart, .connection-chart').exists()).toBe(false)
    expect(chart.findComponent(TimeBadge).exists()).toBe(false)
  })
  it('clears a complete curve when the date range changes', async () => {
    await open(true)
    vi.mocked(fetchTrafficTrends).mockResolvedValue({ ...pending(scope()), from: '2026-08-01', to: '2026-08-02' })
    await router.push(path + '?from=2026-08-01&to=2026-08-02' + (admin ? '&section=details' : '')); await flushPromises()
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    expect(chart.text()).toContain('当前范围统计未知')
    expect(chart.find('.traffic-chart, .connection-chart').exists()).toBe(false)
    expect(chart.findComponent(TimeBadge).exists()).toBe(false)
    expect(fetchTrafficTrends).toHaveBeenCalledTimes(2)
  })
  it('ignores a late complete refresh after the new scope has accepted pending statistics', async () => {
    await open(true)
    const late = deferred<any>()
    vi.mocked(fetchTrafficTrends).mockReturnValueOnce(late.promise).mockResolvedValueOnce(pending(scope({ subscription_id: 2 })))
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    await chart.findAll('button').find(button => button.text() === '刷新趋势')!.trigger('click'); await flushPromises()
    await router.push(path + range + '&subscription_id=2'); await flushPromises()
    late.resolve(trend(1, scope())); await flushPromises()
    expect(chart.text()).toContain('当前范围统计未知')
    expect(chart.find('.traffic-chart, .connection-chart').exists()).toBe(false)
    expect(chart.findComponent(TimeBadge).exists()).toBe(false)
    expect(vi.mocked(fetchTrafficTrends).mock.calls[1]![0].signal?.aborted).toBe(true)
  })
  it.each(['identity', 'permissions', 'display timezone'])('isolates old complete statistics after changing %s', async boundary => {
    await open(true)
    vi.mocked(fetchTrafficTrends).mockResolvedValue(pending(scope({ user_id: admin ? 0 : 2 })))
    if (boundary === 'identity') app.user.id = 2
    else if (boundary === 'permissions') app.user.isAdmin = !app.user.isAdmin
    else setDisplayTimeZone('Asia/Shanghai')
    await flushPromises()
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    expect(chart.text()).toContain('当前范围统计未知')
    expect(chart.find('.traffic-chart, .connection-chart').exists()).toBe(false)
    expect(chart.findComponent(TimeBadge).exists()).toBe(false)
    expect(fetchTrafficTrends).toHaveBeenCalledTimes(2)
  })
  it('does not reuse a snapshot when the server reports a different statistics calendar', async () => {
    await open(true)
    vi.mocked(fetchTrafficTrends).mockResolvedValue(pending(scope({ time_zone: 'Asia/Shanghai' })))
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    await chart.findAll('button').find(button => button.text() === '刷新趋势')!.trigger('click'); await flushPromises()
    expect(chart.text()).toContain('当前范围统计未知')
    expect(chart.find('.traffic-chart, .connection-chart').exists()).toBe(false)
    expect(chart.findComponent(TimeBadge).exists()).toBe(false)
  })
  it('keeps a real server failure distinct from pending while retaining the same-scope old curve', async () => {
    await open(true)
    vi.mocked(fetchTrafficTrends).mockRejectedValueOnce({ response: { status: 500, data: { message: '统计数据库读取失败' } } })
    const chart = wrapper!.getComponent(TrafficObservabilityChart)
    const oldCurve = chart.get('.traffic-line.used').attributes('d')
    await chart.findAll('button').find(button => button.text() === '刷新趋势')!.trigger('click'); await flushPromises()
    expect(wrapper!.text()).toContain('流量趋势加载失败')
    expect(wrapper!.text()).toContain('统计数据库读取失败')
    expect(chart.text()).not.toContain('统计更新中')
    expect(chart.get('.traffic-line.used').attributes('d')).toBe(oldCurve)
    expect(chart.getComponent(TimeBadge).props('value')).toBe('2026-09-01T01:00:00Z')
    expect(fetchTrafficTrends).toHaveBeenCalledTimes(2)
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
    await router.push(path + '?from=2026-01-01&to=2026-02-01&bucket=minute' + (admin ? '&section=details' : ''))
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
