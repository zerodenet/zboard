import type { TrafficUsageBucket } from '../api/trafficUsage'

/** Match the server's bounded node-series windows without changing table granularity. */
export function trafficNodeBucket(bucket: TrafficUsageBucket, from: string, to: string, nodeFiltered = false): TrafficUsageBucket {
  if (bucket === 'day') return 'day'
  const days = (Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86400000 + 1
  if (!Number.isFinite(days) || days <= 0) return 'hour'
  if (bucket === 'minute' && days <= (nodeFiltered ? 7 : 1)) return 'minute'
  return days <= (nodeFiltered ? 366 : 31) ? 'hour' : 'day'
}
