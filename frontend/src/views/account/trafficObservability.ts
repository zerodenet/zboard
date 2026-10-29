import {
  fetchTrafficTrends,
  type TrafficTrendPoint,
  type TrafficTrendResult,
  type TrafficTrendSnapshot,
} from '../../api/readModels'
import type { QueryID } from '../../api/queryID'

// Kept for chart compatibility. Backend aggregation is no longer bounded by a
// browser-side record scan, so this value is never used as a truncation limit.
export const TRAFFIC_OBSERVABILITY_RECORD_LIMIT = Number.MAX_SAFE_INTEGER

export type TrafficObservabilityPoint = TrafficTrendPoint

export type TrafficObservabilityResult = TrafficTrendResult

export interface TrafficObservabilityQuery {
  subscriptionId?: QueryID
  from: string
  to: string
  signal?: AbortSignal
}

export function emptyTrafficObservabilityResult(): TrafficTrendSnapshot {
  return {
    from: '',
    to: '',
    points: [],
    record_count: 0,
    connection_sample_count: 0,
    peak_connections: null,
    truncated: false,
    subscriptions: [],
    as_of: '',
  }
}

export async function fetchTrafficObservability(query: TrafficObservabilityQuery): Promise<TrafficObservabilityResult> {
  const result = await fetchTrafficTrends({
    subscriptionId: query.subscriptionId,
    from: query.from,
    to: query.to,
    signal: query.signal,
  })
  return result
}
