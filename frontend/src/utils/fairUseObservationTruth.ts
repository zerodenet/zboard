import type { FairUseObservationBucket } from '../api/fairUse'

interface CoverageEvidence { telemetry_completeness?: string; coverage?: { state?: string } }
// Both DTO declarations must agree before absence may be interpreted as zero.
// Missing, future or inconsistent coverage states are unproven coverage.
export function fairUseCoverageComplete(evidence: CoverageEvidence | null | undefined): boolean {
  return evidence?.telemetry_completeness === 'complete' && evidence.coverage?.state === 'complete'
}
export function fairUseCoverageState(evidence: CoverageEvidence | null | undefined): 'complete' | 'incomplete' | 'unknown' {
  if (fairUseCoverageComplete(evidence)) return 'complete'
  return evidence?.telemetry_completeness === 'incomplete' || evidence?.coverage?.state === 'incomplete' ? 'incomplete' : 'unknown'
}
export function fairUseObservedNumber(value: unknown, complete: boolean): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) return null
  return value > 0 || complete ? value : null
}
export function fairUseBucketActivity(bucket: Pick<FairUseObservationBucket, 'connection_starts' | 'working_nodes'>, complete: boolean): { label: string; tone: 'info' | 'neutral' | 'warning' } {
  if (fairUseObservedNumber(bucket.connection_starts, false) !== null || fairUseObservedNumber(bucket.working_nodes, false) !== null) return { label: '已见行为', tone: 'info' }
  // flow.started is a start event, not an online snapshot. Even a complete
  // zero-start bucket cannot establish that earlier connections are idle.
  return complete && fairUseObservedNumber(bucket.connection_starts, true) === 0 && fairUseObservedNumber(bucket.working_nodes, true) === 0
    ? { label: '未见新连接', tone: 'neutral' }
    : { label: '活动未知', tone: 'warning' }
}
export function fairUseEvaluationKnown(value: { last_evaluated_at?: string | null; score?: number } | null | undefined): boolean {
  return Boolean(value?.last_evaluated_at && Number.isFinite(Date.parse(value.last_evaluated_at)) && typeof value.score === 'number' && Number.isFinite(value.score) && value.score >= 0)
}
