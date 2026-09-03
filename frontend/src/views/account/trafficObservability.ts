import {
  fetchTrafficTrends,
  type EntityReference,
  type TrafficTrendPoint,
} from '../../api/readModels'
import type { QueryID } from '../../api/queryID'

// Kept for chart compatibility. Backend aggregation is no longer bounded by a
// browser-side record scan, so this value is never used as a truncation limit.
export const TRAFFIC_OBSERVABILITY_RECORD_LIMIT = Number.MAX_SAFE_INTEGER

export type TrafficObservabilityPoint = TrafficTrendPoint

export interface TrafficObservabilityResult {
  points: TrafficObservabilityPoint[]
  record_count: number
  connection_sample_count: number
  peak_connections: number | null
  truncated: boolean
  subscriptions: EntityReference[]
  as_of?: string
}

export interface TrafficObservabilityQuery {
  subscriptionId?: QueryID
  from: string
  to: string
  signal?: AbortSignal
}

export function emptyTrafficObservabilityResult(): TrafficObservabilityResult {
  return {
    points: [],
    record_count: 0,
    connection_sample_count: 0,
    peak_connections: null,
    truncated: false,
    subscriptions: [],
  }
}

export async function fetchTrafficObservability(query: TrafficObservabilityQuery): Promise<TrafficObservabilityResult> {
  const result = await fetchTrafficTrends({
    subscriptionId: query.subscriptionId,
    from: query.from,
    to: query.to,
    signal: query.signal,
  })
  return {
    points: result.points,
    record_count: result.record_count,
    connection_sample_count: result.connection_sample_count,
    peak_connections: result.peak_connections,
    truncated: result.truncated,
    subscriptions: result.subscriptions,
    as_of: result.as_of,
  }
}
