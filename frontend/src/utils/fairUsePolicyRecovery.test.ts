import { describe, expect, it } from 'vitest'
import { fairUsePolicyBaseline, fairUsePolicyDraftFields, fairUsePolicyDraftKeys, fairUsePolicyRecoveryDraft } from './fairUsePolicyRecovery'
const fields = () => ({ evaluation_interval_seconds: 60, connection_start_threshold: 120, connection_start_window_seconds: 60, connection_start_penalty: 10, working_node_threshold: 3, working_node_window_seconds: 300, working_node_penalty: 15, score_max: 100, recovery_per_interval: 8, warning_score: 30, violation_score: 60 })
const resolution = (kind = 'subscription', revision = 4) => ({ configured: true, effective: { ...fields(), scope_type: kind, scope_id: kind === 'platform' ? 0 : kind === 'plan' ? 7 : 2, revision, enabled: true, enforcement_mode: 'observe', restriction_duration_seconds: 300 }, source: { scope_type: kind, scope_id: kind === 'platform' ? 0 : kind === 'plan' ? 7 : 2 } })
describe('Fair Use ordinary policy recovery authority', () => {
  it('captures only the eleven integer draft fields and original scope/version evidence', () => {
    const extras = { enabled: true, execute: true, notify: true, enforcement_mode: 'restrict', restriction_duration_seconds: 42, run_id: 'DO-NOT-STORE', token: 'DO-NOT-STORE' }
    const baseline = fairUsePolicyBaseline(resolution() as any, 2)!
    const draft = fairUsePolicyRecoveryDraft({ subscriptionID: 2, fields: { ...fields(), ...extras }, baseline: { ...baseline, ...extras }, ...extras })!
    expect(draft).toEqual({ subscriptionID: 2, fields: fields(), baseline })
    expect(Object.keys(draft.fields)).toEqual([...fairUsePolicyDraftKeys])
    for (const key of Object.keys(extras)) expect(JSON.stringify(draft)).not.toContain(key)
    expect(JSON.stringify(draft)).not.toContain('DO-NOT-STORE')
  })
  it.each(['platform', 'plan', 'subscription'])('retains inherited %s source identity and original revision', (kind) => {
    const baseline = fairUsePolicyBaseline(resolution(kind, 9) as any, 2)!
    expect(baseline.scope_type).toBe(kind); expect(baseline.source_scope_type).toBe(kind); expect(baseline.revision).toBe(9)
    expect(fairUsePolicyRecoveryDraft({ subscriptionID: 2, fields: fields(), baseline })?.baseline).toEqual(baseline)
  })
  it('rejects missing/unsafe numeric fields while preserving ordinary invalid-range integers for user correction', () => {
    expect(fairUsePolicyDraftFields({ ...fields(), warning_score: 0 })?.warning_score).toBe(0)
    for (const value of [null, undefined, '30', NaN, Infinity, 1.2, Number.MAX_SAFE_INTEGER + 1]) expect(fairUsePolicyDraftFields({ ...fields(), warning_score: value })).toBeUndefined()
    expect(fairUsePolicyRecoveryDraft({ subscriptionID: 0, fields: fields(), baseline: fairUsePolicyBaseline(resolution() as any, 2) })).toBeUndefined()
  })
  it('rejects wrong targets or contradictory source ownership instead of rebasing the old edit', () => {
    const baseline = fairUsePolicyBaseline(resolution() as any, 2)!
    expect(fairUsePolicyRecoveryDraft({ subscriptionID: 3, fields: fields(), baseline })).toBeUndefined()
    expect(fairUsePolicyRecoveryDraft({ subscriptionID: 2, fields: fields(), baseline: { ...baseline, source_scope_type: 'platform' } })).toBeUndefined()
    expect(fairUsePolicyRecoveryDraft({ subscriptionID: 2, fields: fields(), baseline: { ...baseline, source_scope_id: 99 } })).toBeUndefined()
    expect(fairUsePolicyRecoveryDraft({ subscriptionID: 2, fields: fields(), baseline: { ...baseline, scope_type: 'platform', source_scope_type: 'platform', scope_id: 4, source_scope_id: 4 } })).toBeUndefined()
    expect(fairUsePolicyBaseline({ ...resolution(), effective: { ...resolution().effective, revision: -1 } } as any, 2)).toBeUndefined()
  })
})
