import { defineComponent } from 'vue'
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import FairUse from './FairUse.vue'
import { fetchAdminSubscriptionDetail } from '../api/client'
import { fetchFairUseEvents, fetchFairUseMetrics, fetchFairUseObservations, fetchFairUsePolicy, fetchFairUseState, updateSubscriptionFairUsePolicy, evaluateSubscriptionFairUse } from '../api/fairUse'
import { fetchNavigation } from '../api/menus'
import { captureReauthenticationDrafts, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity } from '../utils/reauthenticationDrafts'
const state = vi.hoisted(() => ({ app: null as any }))
vi.mock('../stores/app', () => ({ useAppStore: () => state.app }))
vi.mock('../api/client', () => ({ fetchAdminSubscriptionDetail: vi.fn(), fetchSubscriptionsPage: vi.fn() }))
vi.mock('../api/menus', () => ({ fetchNavigation: vi.fn() }))
vi.mock('../api/fairUse', () => ({ fetchFairUseEvents: vi.fn(), fetchFairUseMetrics: vi.fn(), fetchFairUseObservations: vi.fn(), fetchFairUsePolicy: vi.fn(), fetchFairUseState: vi.fn(), updateSubscriptionFairUsePolicy: vi.fn(), evaluateSubscriptionFairUse: vi.fn() }))
const numeric = () => ({ evaluation_interval_seconds: 60, connection_start_threshold: 120, connection_start_window_seconds: 60, connection_start_penalty: 10, working_node_threshold: 3, working_node_window_seconds: 300, working_node_penalty: 15, score_max: 100, recovery_per_interval: 8, warning_score: 30, violation_score: 60 })
const policy = (revision = 4, kind = 'subscription', scopeID = 2) => ({ configured: true, effective: { ...numeric(), scope_type: kind, scope_id: scopeID, revision, enabled: false, enforcement_mode: 'observe', restriction_duration_seconds: 300 }, source: { scope_type: kind, scope_id: scopeID } })
const modal = defineComponent({ props: ['open'], template: '<section v-if="open"><slot /><slot name="footer" :request-close="() => {}" /></section>' })
let view: VueWrapper | undefined, captured = ''
beforeEach(() => {
  vi.clearAllMocks(); sessionStorage.clear(); clearReauthenticationDrafts(); captured = ''
  state.app = { token: 'verified', user: { id: 1 }, isAdmin: true, reauthRequired: false, sessionEpoch: 0 }
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 1, nodes: [], page_available: true })
  vi.mocked(fetchAdminSubscriptionDetail).mockResolvedValue({ id: 2, user_id: 3, user_email: 'user@example.test', plan_id: 7, plan_name: 'Plan', plan_sku_id: 1, sku_name: 'SKU', status: 'active' } as any)
  vi.mocked(fetchFairUseMetrics).mockResolvedValue({ subscription_id: 2, sampled_at: '', telemetry_completeness: 'unknown', coverage: { state: 'unknown' } } as any)
  vi.mocked(fetchFairUseObservations).mockResolvedValue({ subscription_id: 2, range: '1d', telemetry_completeness: 'unknown', coverage: { state: 'unknown', required_nodes: 1, complete_nodes: 0 }, buckets: [], retention_days: 15, bucket_seconds: 3600 } as any)
  vi.mocked(fetchFairUseState).mockResolvedValue({ subscription_id: 2, score: 0, state: 'normal', telemetry_completeness: 'unknown' } as any)
  vi.mocked(fetchFairUsePolicy).mockImplementation(async () => policy() as any)
  vi.mocked(fetchFairUseEvents).mockResolvedValue([])
  vi.mocked(updateSubscriptionFairUsePolicy).mockResolvedValue(policy(5) as any)
  vi.mocked(evaluateSubscriptionFairUse).mockResolvedValue({ run_id: 'new-run', state: 'queued', subscription_id: 2 })
})
afterEach(() => { view?.unmount(); view = undefined; clearReauthenticationDrafts() })
async function open() {
  const path = '/admin/fair-use?subscription=2'
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/fair-use', component: FairUse }] })
  await router.push(path)
  view = shallowMount(FairUse, { global: { plugins: [router], renderStubDefaultSlot: true, stubs: { ModalDialog: modal, PageAlert: { props: ['title'], template: '<aside><strong>{{title}}</strong><slot /></aside>' } } } })
  await flushPromises()
  return path
}
async function roundTrip(path: string, actor = 1, admin = true) {
  expect(captureReauthenticationDrafts(1, path).ok).toBe(true)
  captured = Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index)!)).join('')
  view!.unmount(); view = undefined; state.app.user.id = actor; state.app.isAdmin = admin
  confirmReauthenticationDraftIdentity(actor, admin)
  await open()
}
function noWrites() { expect(updateSubscriptionFairUsePolicy).not.toHaveBeenCalled(); expect(evaluateSubscriptionFairUse).not.toHaveBeenCalled() }
it('restores ordinary numeric thresholds and original revision without enabling or executing evaluation', async () => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777; vm.policyDraft.warning_score = 20
  vm.policyDraft.enabled = true; vm.policyDraft.execute = true; vm.policyDraft.enforcement_mode = 'restrict'; vm.evaluationReceipt = { run_id: 'DO-NOT-STORE', state: 'queued', subscription_id: 2 }
  await roundTrip(path)
  const restored = view!.vm as any
  expect(restored.policyEditorOpen).toBe(true); expect(restored.policyDraft.connection_start_threshold).toBe(777); expect(restored.policyDraft.warning_score).toBe(20)
  expect(restored.policy.effective.enabled).toBe(false); expect(restored.evaluationReceipt).toBeNull()
  expect(restored.policyOriginalBaseline).toMatchObject({ scope_type: 'subscription', scope_id: 2, revision: 4 })
  for (const marker of ['"enabled"', '"execute"', '"enforcement_mode"', 'DO-NOT-STORE']) expect(captured).not.toContain(marker)
  noWrites()
  await restored.savePolicyParameters()
  expect(updateSubscriptionFairUsePolicy).toHaveBeenCalledWith(2, expect.objectContaining({ connection_start_threshold: 777, warning_score: 20, revision: 4, enforcement_mode: 'observe' }), true)
  expect(evaluateSubscriptionFairUse).toHaveBeenCalledWith(2)
})
it.each(['revision', 'fields', 'source'] as const)('preserves original evidence through repeated authentication when fresh policy %s changes', async (kind) => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  vi.mocked(fetchFairUsePolicy).mockImplementation(async () => (kind === 'source' ? policy(9, 'plan', 7) : kind === 'fields' ? { ...policy(), effective: { ...policy().effective, working_node_threshold: 8 } } : policy(5)) as any)
  await roundTrip(path); await roundTrip(path)
  const restored = view!.vm as any
  expect(restored.policyDraft.connection_start_threshold).toBe(777); expect(restored.policyOriginalBaseline.revision).toBe(4); expect(restored.policyRecoveryConflict).toBe(true)
  await restored.savePolicyParameters(); noWrites()
})
it('does not rebase an open ordinary edit onto a freshly refreshed policy', async () => {
  await open(); const vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  vm.policy = policy(5)
  await vm.savePolicyParameters()
  expect(vm.policyRecoveryConflict).toBe(true); expect(vm.policyDraft.connection_start_threshold).toBe(777); noWrites()
})
it('binds an open edit to its original subscription rather than applying it to a changed target', async () => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777; vm.subscriptionID = 3
  await flushPromises(); await vm.savePolicyParameters()
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false); expect(vm.policyDraft.connection_start_threshold).toBe(777); noWrites()
})
it('retains invalid ordinary integers for correction but refuses unsafe nonnumeric capture without clearing the editor', async () => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.warning_score = 0
  await roundTrip(path)
  const restored = view!.vm as any
  expect(restored.policyDraft.warning_score).toBe(0); noWrites()
  restored.policyDraft.warning_score = 'DO-NOT-STORE'
  await flushPromises()
  expect(captureReauthenticationDrafts(1, path).ok).toBe(false); expect(restored.policyEditorOpen).toBe(true); expect(restored.policyDraft.warning_score).toBe('DO-NOT-STORE'); noWrites()
})
it('waits for fresh policy reads and retains the stored draft on a real read failure', async () => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  vi.mocked(fetchFairUsePolicy).mockRejectedValue({ response: { status: 503 } })
  await roundTrip(path)
  expect((view!.vm as any).policyEditorOpen).toBe(false); expect(sessionStorage.length).toBeGreaterThan(0); noWrites()
})
it('ignores a late policy restore after the owning page was disposed', async () => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  let done!: (value: any) => void, reads = 0
  const pending = new Promise<any>(resolve => { done = resolve })
  vi.mocked(fetchFairUsePolicy).mockImplementation(() => ++reads === 1 ? Promise.resolve(policy() as any) : pending)
  await roundTrip(path)
  const old = view!.vm as any
  view!.unmount(); view = undefined; done(policy()); await flushPromises()
  expect(old.policyEditorOpen).toBe(false); noWrites()
})
it('requires fresh page permission before restoring threshold text', async () => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  vi.mocked(fetchNavigation).mockResolvedValue({ revision: 2, nodes: [], page_available: false })
  await roundTrip(path)
  expect((view!.vm as any).policyEditorOpen).toBe(false); expect(sessionStorage.length).toBe(0); noWrites()
})
it.each([{ actor: 2, admin: true }, { actor: 1, admin: false }])('does not expose old administrator drafts after identity/role change %j', async ({ actor, admin }) => {
  const path = await open(), vm = view!.vm as any
  vm.openPolicyEditor(); vm.policyDraft.connection_start_threshold = 777
  await roundTrip(path, actor, admin)
  expect((view!.vm as any).policyEditorOpen).toBe(false); expect(sessionStorage.length).toBe(0); noWrites()
})
