import { describe, expect, it } from 'vitest'
import {
  aggregateTrafficObservability,
  resolvePeakConnections,
  type TrafficObservabilityRecord,
} from './trafficObservability'

function record(overrides: Partial<TrafficObservabilityRecord>): TrafficObservabilityRecord {
  return {
    id: 1,
    user_id: 9,
    subscription_id: 12,
    node_id: 3,
    protocol_endpoint_id: 7,
    raw_bytes: 0,
    upload_bytes: 0,
    download_bytes: 0,
    protocol_multiplier_milli: 1000,
    used_bytes: 0,
    record_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

describe('account traffic observability', () => {
  it('sums daily traffic while keeping the highest connection sample', () => {
    const result = aggregateTrafficObservability([
      record({ id: 1, upload_bytes: 100, download_bytes: 300, used_bytes: 400, peak_connections: 7 }),
      record({ id: 2, upload_bytes: 200, download_bytes: 500, used_bytes: 700, peak_connections: 13 }),
      record({ id: 3, record_at: '2026-08-03T10:00:00Z', upload_bytes: 50, download_bytes: 150, used_bytes: 200 }),
    ], '2026-08-01', '2026-08-03')

    expect(result.points).toHaveLength(3)
    expect(result.points[0]).toMatchObject({
      date: '2026-08-01',
      upload_bytes: 300,
      download_bytes: 800,
      used_bytes: 1100,
      peak_connections: 13,
    })
    expect(result.points[1]).toMatchObject({
      date: '2026-08-02',
      upload_bytes: 0,
      download_bytes: 0,
      used_bytes: 0,
      peak_connections: null,
    })
    expect(result.peak_connections).toBe(13)
    expect(result.connection_sample_count).toBe(2)
  })

  it('treats zero as a valid peak and missing data as unavailable', () => {
    expect(resolvePeakConnections(record({ peak_connections: 0 }))).toBe(0)
    expect(resolvePeakConnections(record({ peak_connections: null }))).toBeNull()
    expect(resolvePeakConnections(record({}))).toBeNull()
  })

  it('does not invent connection samples when the kernel has not reported them', () => {
    const result = aggregateTrafficObservability([
      record({ used_bytes: 1024 }),
    ], '2026-08-01', '2026-08-01')

    expect(result.points[0].peak_connections).toBeNull()
    expect(result.peak_connections).toBeNull()
    expect(result.connection_sample_count).toBe(0)
  })
})
