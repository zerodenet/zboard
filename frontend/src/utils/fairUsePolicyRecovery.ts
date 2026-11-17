import type { FairUsePolicyResolution } from '../api/fairUse'

export const fairUsePolicyDraftKeys = ['evaluation_interval_seconds', 'connection_start_threshold', 'connection_start_window_seconds', 'connection_start_penalty', 'working_node_threshold', 'working_node_window_seconds', 'working_node_penalty', 'score_max', 'recovery_per_interval', 'warning_score', 'violation_score'] as const
export type FairUsePolicyDraftFields = Record<typeof fairUsePolicyDraftKeys[number], number>
const record = (v: unknown): v is Record<string, unknown> => Boolean(v) && typeof v === 'object' && !Array.isArray(v)
const id = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) >= 0
const scope = (v: unknown): v is 'platform' | 'plan' | 'subscription' => v === 'platform' || v === 'plan' || v === 'subscription'
export function fairUsePolicyDraftFields(v: unknown): FairUsePolicyDraftFields | undefined {
  if (!record(v)) return
  const fields = {} as FairUsePolicyDraftFields
  for (const key of fairUsePolicyDraftKeys) {
    if (!Number.isSafeInteger(v[key])) return
    fields[key] = Number(v[key])
  }
  return fields
}
export interface FairUsePolicyRecoveryBaseline {
  scope_type: 'platform' | 'plan' | 'subscription'; scope_id: number; revision: number
  source_scope_type: 'platform' | 'plan' | 'subscription'; source_scope_id: number
  fields: FairUsePolicyDraftFields
}
export function fairUsePolicyRecoveryBaseline(v: unknown, subscriptionID: number): FairUsePolicyRecoveryBaseline | undefined {
  if (!record(v) || !scope(v.scope_type) || !scope(v.source_scope_type) || !id(v.scope_id) || !id(v.source_scope_id) || !id(v.revision)) return
  const fields = fairUsePolicyDraftFields(v.fields)
  if (!fields || v.scope_type !== v.source_scope_type || v.scope_id !== v.source_scope_id || (v.scope_type === 'platform' && v.scope_id !== 0) || (v.scope_type === 'plan' && v.scope_id === 0) || (v.scope_type === 'subscription' && v.scope_id !== subscriptionID)) return
  return { scope_type: v.scope_type, scope_id: v.scope_id, revision: v.revision, source_scope_type: v.source_scope_type, source_scope_id: v.source_scope_id, fields }
}
export function fairUsePolicyBaseline(resolution: FairUsePolicyResolution | null | undefined, subscriptionID: number): FairUsePolicyRecoveryBaseline | undefined {
  if (!resolution?.effective || !resolution.source) return
  return fairUsePolicyRecoveryBaseline({ scope_type: resolution.effective.scope_type, scope_id: resolution.effective.scope_id, revision: resolution.effective.revision,
    source_scope_type: resolution.source.scope_type, source_scope_id: resolution.source.scope_id, fields: resolution.effective }, subscriptionID)
}
export interface FairUsePolicyRecoveryDraft { subscriptionID: number; fields: FairUsePolicyDraftFields; baseline: FairUsePolicyRecoveryBaseline }
export function fairUsePolicyRecoveryDraft(v: unknown): FairUsePolicyRecoveryDraft | undefined {
  if (!record(v) || !id(v.subscriptionID) || v.subscriptionID <= 0) return
  const fields = fairUsePolicyDraftFields(v.fields), baseline = fairUsePolicyRecoveryBaseline(v.baseline, v.subscriptionID)
  if (!fields || !baseline) return
  // This object has no enabled, enforcement, notify, execute, receipt or other
  // authorization fields, even when the caller's form contains them.
  return { subscriptionID: v.subscriptionID, fields, baseline }
}
