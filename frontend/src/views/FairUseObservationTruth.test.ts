import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import FairUse from './FairUse.vue'
import { fetchAdminSubscriptionDetail } from '../api/client'
import { fetchFairUseEvents, fetchFairUseMetrics, fetchFairUseObservations, fetchFairUsePolicy, fetchFairUseState, updateSubscriptionFairUsePolicy, evaluateSubscriptionFairUse } from '../api/fairUse'
vi.mock('../stores/app', () => ({ useAppStore: () => ({ token: '', user: { id: 0 }, reauthRequired: false, sessionEpoch: 0 }) }))
vi.mock('../api/client', () => ({ fetchAdminSubscriptionDetail: vi.fn(), fetchSubscriptionsPage: vi.fn() }))
vi.mock('../api/fairUse', () => ({ fetchFairUseEvents: vi.fn(), fetchFairUseMetrics: vi.fn(), fetchFairUseObservations: vi.fn(), fetchFairUsePolicy: vi.fn(), fetchFairUseState: vi.fn(), updateSubscriptionFairUsePolicy: vi.fn(), evaluateSubscriptionFairUse: vi.fn() }))
const coverage = (state: string) => ({ state, reason: 'one_or_more_nodes_unknown', window_seconds: 86400, required_nodes: 1, complete_nodes: state === 'complete' ? 1 : 0, nodes: [] })
const bucket = (start: string, count: number, nodes: number) => ({ start_at: start, end_at: start, connection_starts: count, working_nodes: nodes })
const observation = (state: string, positive = false) => ({ subscription_id: 1, range: '1d', since: '2026-10-26T00:00:00Z', until: '2026-10-27T00:00:00Z', bucket_seconds: 3600, retention_days: 15, time_basis: 'received', telemetry_completeness: state, coverage: coverage(state), total_connection_starts: positive ? 8 : 0, distinct_working_nodes: positive ? 2 : 0, active_buckets: positive ? 1 : 0, max_connection_starts_per_bucket: positive ? 8 : 0, p50_connection_starts_per_bucket: 0, p95_connection_starts_per_bucket: positive ? 8 : 0, max_working_nodes_per_bucket: positive ? 2 : 0, p50_working_nodes_per_bucket: 0, p95_working_nodes_per_bucket: positive ? 2 : 0,
  buckets: [bucket('2026-10-26T01:00:00Z', 0, 0), ...(positive ? [bucket('2026-10-26T02:00:00Z', 8, 2)] : [])] })
const metric = defineComponent({ props: ['label', 'value', 'status', 'meta'], template: '<article class="metric"><h3>{{label}}</h3><strong>{{value}}</strong><small>{{status}}</small><p>{{meta}}</p></article>' })
const section = defineComponent({ props: ['title', 'description'], template: '<section><h2>{{title}}</h2><p>{{description}}</p><div class="section-meta"><slot name="meta" /></div><slot /></section>' })
let view: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(fetchAdminSubscriptionDetail).mockResolvedValue({ id: 1, user_id: 1, user_email: 'user@example.test', plan_id: 1, plan_name: 'Plan', plan_sku_id: 1, sku_name: 'SKU', status: 'active' } as any)
  vi.mocked(fetchFairUseMetrics).mockResolvedValue({ subscription_id: 1, sampled_at: '', telemetry_completeness: 'unknown', coverage: coverage('unknown'), current_active_flows: 0 } as any)
  vi.mocked(fetchFairUseObservations).mockResolvedValue(observation('unknown') as any)
  vi.mocked(fetchFairUseState).mockResolvedValue({ subscription_id: 1, score: 0, state: 'normal', telemetry_completeness: 'unknown' } as any)
  vi.mocked(fetchFairUsePolicy).mockResolvedValue({ configured: false, source: { scope_type: 'platform', scope_id: 0 }, effective: { enabled: false, score_max: 100, warning_score: 30, violation_score: 60, connection_start_window_seconds: 60, connection_start_threshold: 120, working_node_window_seconds: 300, working_node_threshold: 3, evaluation_interval_seconds: 60 } } as any)
  vi.mocked(fetchFairUseEvents).mockResolvedValue([])
})
afterEach(() => { view?.unmount(); view = undefined })
async function open() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/fair-use', component: FairUse }] })
  await router.push('/admin/fair-use?subscription=1')
  view = shallowMount(FairUse, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: { MetricCard: metric, UiSection: section, PageAlert: { props: ['title'], template: '<aside><strong>{{title}}</strong><slot /></aside>' }, EmptyState: { props: ['title', 'description'], template: '<aside><strong>{{title}}</strong><p>{{description}}</p></aside>' }, ModalDialog: true } } })
  await flushPromises()
  return view
}
function metrics() { return view!.findAll('.metric') }
function noWrites() { expect(updateSubscriptionFairUsePolicy).not.toHaveBeenCalled(); expect(evaluateSubscriptionFairUse).not.toHaveBeenCalled() }
it.each(['unknown', 'incomplete'])('does not claim idle, zero overview or zero distribution for %s coverage', async (state) => {
  vi.mocked(fetchFairUseObservations).mockResolvedValue(observation(state) as any)
  const v = await open()
  expect(v.text()).toContain('活动未知'); expect(v.text()).not.toContain('空闲'); expect(v.text()).toContain('尚未评估')
  for (const card of metrics()) expect(card.find('strong').text()).toBe('未知')
  expect(v.text()).toContain('P50 未知 · P95 未知 · Max 未知')
  expect(v.text()).toContain('未评估'); expect((v.vm as any).observation.buckets).toHaveLength(1); noWrites()
})
it('retains every positive received sample while qualifying incomplete distributions and absent buckets', async () => {
  vi.mocked(fetchFairUseObservations).mockResolvedValue(observation('incomplete', true) as any)
  vi.mocked(fetchFairUseMetrics).mockResolvedValue({ telemetry_completeness: 'incomplete', coverage: coverage('incomplete'), current_active_flows: 3 } as any)
  const v = await open()
  expect(metrics().map(card => card.find('strong').text())).toEqual(['3', '8', '8', '2'])
  expect(v.text()).toContain('已收到样本分布（覆盖不足）'); expect(v.text()).toContain('P50 未知 · P95 8 · Max 8')
  expect(v.text()).toContain('已见行为'); expect(v.text()).toContain('活动未知'); expect(v.text()).not.toContain('空闲')
  expect((v.vm as any).observation.buckets).toHaveLength(2); expect((v.vm as any).observation.total_connection_starts).toBe(8); noWrites()
})
it('keeps proven zero counts and an evaluated zero score, without inferring idle existing connections', async () => {
  vi.mocked(fetchFairUseObservations).mockResolvedValue(observation('complete') as any)
  vi.mocked(fetchFairUseMetrics).mockResolvedValue({ telemetry_completeness: 'complete', coverage: coverage('complete'), current_active_flows: 0 } as any)
  vi.mocked(fetchFairUseState).mockResolvedValue({ score: 0, state: 'normal', last_evaluated_at: '2026-10-27T00:00:00Z', telemetry_completeness: 'complete' } as any)
  const v = await open()
  expect(metrics().map(card => card.find('strong').text())).toEqual(['0', '0', '0', '0'])
  expect(v.text()).toContain('P50 0 · P95 0 · Max 0'); expect(v.text()).toContain('未见新连接'); expect(v.text()).not.toContain('空闲')
  expect(v.find('.experiment-score strong').text()).toBe('0'); expect(v.text()).toContain('基线'); noWrites()
})
it('preserves the last evaluated nonzero score as historical while current coverage is unknown', async () => {
  vi.mocked(fetchFairUseState).mockResolvedValue({ score: 72, state: 'violated', last_evaluated_at: '2026-10-26T23:00:00Z', telemetry_completeness: 'incomplete' } as any)
  const v = await open()
  expect(v.find('.experiment-score strong').text()).toBe('72'); expect(v.text()).toContain('上次已保存的实验评分'); expect(v.text()).toContain('高风险（实验）'); noWrites()
})
it('does not accept contradictory coverage declarations as a complete sample', async () => {
  const value = observation('complete'); value.coverage.state = 'incomplete'
  vi.mocked(fetchFairUseObservations).mockResolvedValue(value as any)
  const v = await open()
  expect(v.text()).toContain('Coverage 不完整'); expect(v.text()).toContain('活动未知'); expect(metrics()[1]!.find('strong').text()).toBe('未知'); noWrites()
})
it('uses an explicit unknown empty-state when no buckets were received under missing coverage', async () => {
  const value = observation('unknown'); value.buckets = []
  vi.mocked(fetchFairUseObservations).mockResolvedValue(value as any)
  const v = await open()
  expect(v.text()).toContain('当前范围连接活动未知'); expect(v.text()).toContain('未收到事件不能当作无活动或零用量'); expect(v.text()).not.toContain('当前范围没有连接启动记录'); noWrites()
})
