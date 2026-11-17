import { describe, expect, it } from 'vitest'
import { fairUseCoverageComplete, fairUseCoverageState, fairUseObservedNumber, fairUseBucketActivity, fairUseEvaluationKnown } from './fairUseObservationTruth'
describe('Fair Use absence and observed-sample truth', () => {
  it.each(['unknown', 'incomplete', '', 'future-state'])('keeps %s absence unknown, without discarding observed nonzero counts', (state) => {
    const dto = { telemetry_completeness: state, coverage: { state } }
    expect(fairUseCoverageComplete(dto)).toBe(false)
    expect(fairUseObservedNumber(0, false)).toBeNull()
    expect(fairUseObservedNumber(17, false)).toBe(17)
    expect(fairUseBucketActivity({ connection_starts: 0, working_nodes: 0 }, false)).toEqual({ label: '活动未知', tone: 'warning' })
    expect(fairUseBucketActivity({ connection_starts: 3, working_nodes: 1 }, false)).toEqual({ label: '已见行为', tone: 'info' })
  })
  it('only confirms zero when both coverage declarations agree and the counter is present', () => {
    expect(fairUseCoverageComplete({ telemetry_completeness: 'complete', coverage: { state: 'complete' } })).toBe(true)
    expect(fairUseCoverageComplete({ telemetry_completeness: 'complete', coverage: { state: 'unknown' } })).toBe(false)
    expect(fairUseCoverageComplete({ telemetry_completeness: 'complete' })).toBe(false)
    expect(fairUseCoverageState({ telemetry_completeness: 'complete', coverage: { state: 'incomplete' } })).toBe('incomplete')
    expect(fairUseObservedNumber(0, true)).toBe(0)
    for (const value of [null, undefined, NaN, Infinity, -1, '0']) expect(fairUseObservedNumber(value, true)).toBeNull()
  })
  it('does not equate zero starts with idle earlier connections, even with complete coverage', () => {
    expect(fairUseBucketActivity({ connection_starts: 0, working_nodes: 0 }, true)).toEqual({ label: '未见新连接', tone: 'neutral' })
    expect(fairUseBucketActivity({ connection_starts: 0, working_nodes: 1 }, false)).toEqual({ label: '已见行为', tone: 'info' })
    expect(fairUseBucketActivity({ connection_starts: undefined as any, working_nodes: 0 }, true).label).toBe('活动未知')
  })
  it('does not manufacture a zero baseline before any saved evaluation', () => {
    expect(fairUseEvaluationKnown({ score: 0 })).toBe(false)
    expect(fairUseEvaluationKnown({ score: 0, last_evaluated_at: 'invalid' })).toBe(false)
    expect(fairUseEvaluationKnown({ score: NaN, last_evaluated_at: '2026-10-27T00:00:00Z' })).toBe(false)
    expect(fairUseEvaluationKnown({ score: 0, last_evaluated_at: '2026-10-27T00:00:00Z' })).toBe(true)
    expect(fairUseEvaluationKnown({ score: 72, last_evaluated_at: '2026-10-27T00:00:00Z' })).toBe(true)
  })
})
