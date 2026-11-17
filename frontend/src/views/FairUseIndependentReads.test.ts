import { defineComponent, reactive } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import FairUse from './FairUse.vue'
import { fetchAdminSubscriptionDetail } from '../api/client'
import { fetchFairUseEvents, fetchFairUseMetrics, fetchFairUseObservations, fetchFairUsePolicy, fetchFairUseState, updateSubscriptionFairUsePolicy, evaluateSubscriptionFairUse } from '../api/fairUse'
import { fetchNavigation } from '../api/menus'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'

const context = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => context.app }))
vi.mock('../api/client', () => ({ fetchAdminSubscriptionDetail: vi.fn(), fetchSubscriptionsPage: vi.fn() }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn() }))
vi.mock('../api/fairUse', () => ({ fetchFairUseEvents: vi.fn(), fetchFairUseMetrics: vi.fn(), fetchFairUseObservations: vi.fn(), fetchFairUsePolicy: vi.fn(), fetchFairUseState: vi.fn(), updateSubscriptionFairUsePolicy: vi.fn(), evaluateSubscriptionFairUse: vi.fn() }))

const coverage = () => ({ state: 'complete', reason: 'all_active_credential_nodes_continuous', window_seconds: 86400, required_nodes: 1, complete_nodes: 1, nodes: [] })
const metrics = (id = 1, count = 6) => ({ subscription_id: id, current_active_flows: count, telemetry_completeness: 'complete', coverage: coverage(), last_received_at: '2026-10-27T01:00:00Z' })
const observation = (id = 1, count = 7, range = '1d') => ({ subscription_id: id, range, telemetry_completeness: 'complete', coverage: coverage(), retention_days: 15, bucket_seconds: 300, buckets: [], total_connection_starts: count, active_buckets: 1, distinct_working_nodes: 1, p50_connection_starts_per_bucket: count, p95_connection_starts_per_bucket: count, max_connection_starts_per_bucket: count, p95_working_nodes_per_bucket: 1 })
const numeric = () => ({ evaluation_interval_seconds: 60, connection_start_threshold: 120, connection_start_window_seconds: 60, connection_start_penalty: 10, working_node_threshold: 3, working_node_window_seconds: 300, working_node_penalty: 15, score_max: 100, recovery_per_interval: 8, warning_score: 30, violation_score: 60 })
const policy = (id = 1, revision = 4) => ({ configured: true, source: { scope_type: 'subscription', scope_id: id }, effective: { ...numeric(), scope_type: 'subscription', scope_id: id, revision, enabled: true, enforcement_mode: 'observe', restriction_duration_seconds: 300 } })
const score = (id = 1, value = 72) => ({ subscription_id: id, score: value, state: 'violated', last_evaluated_at: '2026-10-27T00:00:00Z', telemetry_completeness: 'complete' })
const events = (id = 1, reason = 'saved independent explanation') => [{ id, subscription_id: id, event_type: 'risk_increased', score_before: 0, score_after: 72, state_before: 'normal', state_after: 'violated', occurred_at: '2026-10-27T00:00:00Z', reason, metrics: {} }]
const pending = () => ({ response: { status: 503, data: { message: 'window pending', error: { version: 1, code: 'usage_projection_pending' } } } })
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { resolve, promise } }
const section = defineComponent({ props: ['title', 'description'], template: '<section><h2>{{title}}</h2><p>{{description}}</p><slot name="meta" /><slot /></section>' })
const metric = defineComponent({ props: ['label', 'value', 'status', 'meta'], template: '<article class="metric"><h3>{{label}}</h3><strong>{{value}}</strong><small>{{status}}</small><p>{{meta}}</p></article>' })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined, router: Router
beforeEach(() => {
  vi.resetAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts()
  context.app = reactive({ token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 })
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 1, nodes: [], page_available: true })
  vi.mocked(fetchAdminSubscriptionDetail).mockImplementation(async id => ({ id, user_id: 9, user_email: 'scope@example.test', plan_id: 2, plan_name: 'Scoped plan', plan_sku_id: 3, sku_name: 'Monthly', status: 'active' } as any))
  vi.mocked(fetchFairUseMetrics).mockImplementation(async id => metrics(id) as any)
  vi.mocked(fetchFairUseObservations).mockImplementation(async (id, range) => observation(id, 7, range) as any)
  vi.mocked(fetchFairUseState).mockImplementation(async id => score(id) as any)
  vi.mocked(fetchFairUsePolicy).mockImplementation(async id => policy(id) as any)
  vi.mocked(fetchFairUseEvents).mockImplementation(async id => events(id) as any)
  vi.mocked(updateSubscriptionFairUsePolicy).mockImplementation(async id => policy(id, 5) as any)
  vi.mocked(evaluateSubscriptionFairUse).mockImplementation(async id => ({ subscription_id: id, run_id: 'explicit-run', state: 'queued' }))
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts(); vi.restoreAllMocks() })
async function open(path = '/admin/fair-use?subscription=1') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/fair-use', component: FairUse }] })
  await router.push(path)
  view = shallowMount(FairUse, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: { TableText: false, UiSection: section, MetricCard: metric, ModalDialog: modal, PageAlert: { props: ['title'], template: '<aside><strong>{{title}}</strong><slot /></aside>' }, EmptyState: { props: ['title', 'description'], template: '<aside><strong>{{title}}</strong><p>{{description}}</p></aside>' } } } })
  await flushPromises()
  return view
}
function noWrites() { expect(updateSubscriptionFairUsePolicy).not.toHaveBeenCalled(); expect(evaluateSubscriptionFairUse).not.toHaveBeenCalled() }

it('renders independent policy, saved score and events when both statistical projections are pending', async () => {
  vi.mocked(fetchFairUseMetrics).mockRejectedValue(pending()); vi.mocked(fetchFairUseObservations).mockRejectedValue(pending())
  const v = await open(), vm = v.vm as any
  expect(v.findAll('.metric').map(card => card.find('strong').text())).toEqual(['未知', '未知', '未知', '未知'])
  expect(v.text()).toContain('窗口观察正在更新'); expect(v.text()).toContain('连接观察正在更新')
  expect(v.get('.experiment-score strong').text()).toBe('72'); expect(v.text()).toContain('saved independent explanation')
  expect(vm.policyReadReady).toBe(true); vm.openPolicyEditor(); expect(vm.policyEditorOpen).toBe(true)
  noWrites()
})
it('does not wait for slow statistics before independently permitting verified ordinary policy editing', async () => {
  const slowMetrics = deferred<any>(), slowObservation = deferred<any>()
  vi.mocked(fetchFairUseMetrics).mockReturnValue(slowMetrics.promise); vi.mocked(fetchFairUseObservations).mockReturnValue(slowObservation.promise)
  const v = await open(), vm = v.vm as any
  expect(vm.loading).toBe(true); expect(vm.reads.policy.loading).toBe(false); expect(vm.policyReadReady).toBe(true)
  expect(v.text()).toContain('saved independent explanation'); vm.openPolicyEditor(); expect(vm.policyEditorOpen).toBe(true); noWrites()
  slowMetrics.resolve(metrics()); slowObservation.resolve(observation()); await flushPromises()
  expect(vm.loading).toBe(false)
})
it('permits the existing explicit policy save while projection pending without auto-enabling or replaying a write', async () => {
  vi.mocked(fetchFairUseMetrics).mockRejectedValue(pending()); vi.mocked(fetchFairUseObservations).mockRejectedValue(pending())
  const v = await open(), vm = v.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 321; noWrites()
  await vm.savePolicyParameters()
  expect(updateSubscriptionFairUsePolicy).toHaveBeenCalledOnce()
  expect(updateSubscriptionFairUsePolicy).toHaveBeenCalledWith(1, expect.objectContaining({ revision: 4, connection_start_threshold: 321, enforcement_mode: 'observe' }), true)
  expect(evaluateSubscriptionFairUse).toHaveBeenCalledOnce(); expect(evaluateSubscriptionFairUse).toHaveBeenCalledWith(1)
})
it('preserves known observations with a stale label while independent current records refresh', async () => {
  const v = await open(), vm = v.vm as any
  vi.mocked(fetchFairUseMetrics).mockRejectedValue({ message: 'metrics network timeout' }); vi.mocked(fetchFairUseObservations).mockRejectedValue(pending())
  vi.mocked(fetchFairUseState).mockResolvedValue(score(1, 77) as any); vi.mocked(fetchFairUseEvents).mockResolvedValue(events(1, 'new independent event') as any)
  await vm.load()
  expect(v.findAll('.metric').map(card => card.find('strong').text())).toEqual(['6', '7', '7', '1'])
  expect(v.text()).toContain('保留上次读取结果'); expect(v.text()).toContain('上次读取 · 待刷新'); expect(v.text()).toContain('metrics network timeout')
  expect(v.get('.experiment-score strong').text()).toBe('77'); expect(v.text()).toContain('new independent event')
  expect(vm.reads.metrics.pending).toBe(false); expect(vm.reads.observation.pending).toBe(true); expect(vm.policyReadReady).toBe(true); noWrites()
})
it('retains an ordinary policy draft but blocks open, save and disable after the fresh policy read fails', async () => {
  const v = await open(), vm = v.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  vi.mocked(fetchFairUsePolicy).mockRejectedValue({ response: { status: 403, data: { message: 'policy permission denied' } } })
  await vm.load()
  expect(vm.policy.effective.revision).toBe(4); expect(vm.policyDraft.connection_start_threshold).toBe(777); expect(vm.policyReadReady).toBe(false)
  expect(v.text()).toContain('policy permission denied'); expect(v.text()).toContain('保留上次读取结果')
  await vm.savePolicyParameters(); await vm.setEvaluationEnabled(false); noWrites()
  vm.closePolicyEditor(); vm.openPolicyEditor(); expect(vm.policyEditorOpen).toBe(false)
})
it('does not let an unavailable score block a freshly read policy or independent explanation events', async () => {
  vi.mocked(fetchFairUseState).mockRejectedValue({ message: 'score unavailable' })
  const v = await open(), vm = v.vm as any
  expect(v.get('.experiment-score strong').text()).toBe('未知'); expect(v.text()).toContain('score unavailable')
  expect(v.text()).toContain('saved independent explanation'); expect(vm.policyReadReady).toBe(true)
  vm.openPolicyEditor(); expect(vm.policyEditorOpen).toBe(true); noWrites()
})
it.each([
  { status: 403, error: { version: 1, code: 'permission_denied' } },
  { status: 500, error: { version: 1, code: 'usage_projection_pending' } },
  { status: 503, error: { version: 2, code: 'usage_projection_pending' } },
  { status: 503, error: { version: 1, code: 'unavailable' } },
])('does not disguise another failure as projection pending: %j', async ({ status, error }) => {
  vi.mocked(fetchFairUseMetrics).mockRejectedValue({ response: { status, data: { message: 'actual metrics failure', error } } })
  const v = await open(), vm = v.vm as any
  expect(vm.reads.metrics.pending).toBe(false); expect(v.get('[data-read-problem="metrics"]').text()).toContain('actual metrics failure')
  expect(v.findAll('.metric')[0]!.find('strong').text()).toBe('未知'); expect(vm.policyReadReady).toBe(true); noWrites()
})
it('clears the previous subscription and ignores its late responses while the replacement scope stays current', async () => {
  const oldMetrics = deferred<any>(), oldPolicy = deferred<any>()
  vi.mocked(fetchFairUseMetrics).mockImplementation(id => id === 1 ? oldMetrics.promise : Promise.resolve(metrics(id, 17) as any))
  vi.mocked(fetchFairUsePolicy).mockImplementation(id => id === 1 ? oldPolicy.promise : Promise.resolve(policy(id) as any))
  const v = await open(), vm = v.vm as any
  await router.push('/admin/fair-use?subscription=2'); await flushPromises()
  expect(vm.metrics.subscription_id).toBe(2); expect(vm.policy.effective.scope_id).toBe(2)
  oldMetrics.resolve(metrics(1, 99)); oldPolicy.resolve(policy(1, 99)); await flushPromises()
  expect(vm.metrics.current_active_flows).toBe(17); expect(vm.policy.effective.scope_id).toBe(2); expect(vm.policyReadReady).toBe(true); noWrites()
})
it('does not show an old range or accept its late response while the next range is still loading', async () => {
  const v = await open(), vm = v.vm as any, oldRange = deferred<any>(), nextRange = deferred<any>()
  vi.mocked(fetchFairUseObservations).mockImplementation((_id, range) => range === '1d' ? oldRange.promise : nextRange.promise)
  const oldLoad = vm.load()
  await flushPromises(); await router.push('/admin/fair-use?subscription=1&range=3d'); await flushPromises()
  expect(vm.observation).toBeNull(); expect(v.findAll('.metric')[1]!.find('strong').text()).toBe('未知'); expect(vm.policyReadReady).toBe(true)
  oldRange.resolve(observation(1, 99, '1d')); await oldLoad; await flushPromises()
  expect(vm.observation).toBeNull()
  nextRange.resolve(observation(1, 33, '3d')); await flushPromises()
  expect(vm.observation.range).toBe('3d'); expect(vm.observation.total_connection_starts).toBe(33); noWrites()
})
it('aborts disposed reads and never commits a late response into the previous component', async () => {
  const slow = deferred<any>(); vi.mocked(fetchFairUseMetrics).mockReturnValue(slow.promise)
  const v = await open(), vm = v.vm as any
  const signal = vi.mocked(fetchFairUseMetrics).mock.calls[0]![1]!
  v.unmount(); view = undefined; expect(signal.aborted).toBe(true)
  slow.resolve(metrics(1, 99)); await flushPromises(); expect(vm.metrics).toBeNull(); noWrites()
})
it('keeps a changed-account response out of the old page and does not reuse its policy authority', async () => {
  const v = await open(), vm = v.vm as any, slow = deferred<any>()
  vi.mocked(fetchFairUseMetrics).mockReturnValue(slow.promise); const load = vm.load(); await flushPromises()
  context.app.user.id = 2; context.app.token = 'different'; context.app.sessionEpoch++
  await flushPromises(); slow.resolve(metrics(1, 99)); await load; await flushPromises()
  expect(vm.metrics).toBeNull(); expect(vm.policy).toBeNull(); expect(vm.events).toEqual([]); expect(vm.policyReadReady).toBe(false); noWrites()
})
it('restores verified ordinary draft fields before optional statistical requests finish', async () => {
  let v = await open(), vm = v.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  expect(captureReauthenticationDrafts(1, '/admin/fair-use?subscription=1').ok).toBe(true)
  v.unmount(); view = undefined
  const slowMetrics = deferred<any>(), slowObservation = deferred<any>()
  vi.mocked(fetchFairUseMetrics).mockReturnValue(slowMetrics.promise); vi.mocked(fetchFairUseObservations).mockReturnValue(slowObservation.promise)
  confirmReauthenticationDraftIdentity(1, true)
  v = await open(); vm = v.vm as any
  expect(vm.loading).toBe(true); expect(vm.policyEditorOpen).toBe(true); expect(vm.policyDraft.connection_start_threshold).toBe(777)
  expect(vm.policyOriginalBaseline.revision).toBe(4); noWrites()
  slowMetrics.resolve(metrics()); slowObservation.resolve(observation()); await flushPromises()
})
it('does not enqueue evaluation for another subscription when an explicit policy write finishes late', async () => {
  const v = await open(), vm = v.vm as any, slow = deferred<any>()
  vm.openPolicyEditor(); vi.mocked(updateSubscriptionFairUsePolicy).mockReturnValue(slow.promise)
  const saving = vm.savePolicyParameters(); await flushPromises()
  expect(updateSubscriptionFairUsePolicy).toHaveBeenCalledOnce()
  await router.push('/admin/fair-use?subscription=2'); await flushPromises()
  slow.resolve(policy(1, 5)); await saving; await flushPromises()
  expect(vm.policy.effective.scope_id).toBe(2); expect(evaluateSubscriptionFairUse).not.toHaveBeenCalled()
})

it('fetches the selected range when a control updates its model before synchronizing the URL', async () => {
  const v = await open(), vm = v.vm as any
  vi.mocked(fetchFairUseObservations).mockClear()
  vm.range = '3d'; await vm.applyRange()
  expect(router.currentRoute.value.query.range).toBe('3d')
  expect(fetchFairUseObservations).toHaveBeenCalledOnce()
  expect(fetchFairUseObservations).toHaveBeenCalledWith(1, '3d', expect.any(AbortSignal)); noWrites()
})
it('keeps newer mounted ordinary input when permission verification for a stored draft finishes late', async () => {
  let v = await open(), vm = v.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  expect(captureReauthenticationDrafts(1, '/admin/fair-use?subscription=1').ok).toBe(true)
  v.unmount(); view = undefined
  const navigation = deferred<any>(); vi.mocked(fetchNavigation).mockReturnValue(navigation.promise)
  confirmReauthenticationDraftIdentity(1, true)
  v = await open(); vm = v.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 888
  navigation.resolve({ revision: 1, nodes: [], page_available: true }); await flushPromises()
  expect(vm.policyDraft.connection_start_threshold).toBe(888); expect(vm.policyOriginalBaseline.revision).toBe(4); noWrites()
})
