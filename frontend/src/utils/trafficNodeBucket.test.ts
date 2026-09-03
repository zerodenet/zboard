import { describe, expect, it } from 'vitest'
import { trafficNodeBucket } from './trafficNodeBucket'

describe('bounded node chart resolution', () => {
  it('keeps a one-day minute range and coarsens a multi-node second day', () => {
    expect(trafficNodeBucket('minute', '2026-01-01', '2026-01-01')).toBe('minute')
    expect(trafficNodeBucket('minute', '2026-01-01', '2026-01-02')).toBe('hour')
  })
  it('permits seven-minute-resolution days only for a single node', () => {
    expect(trafficNodeBucket('minute', '2026-01-01', '2026-01-07', true)).toBe('minute')
    expect(trafficNodeBucket('minute', '2026-01-01', '2026-01-08', true)).toBe('hour')
  })
  it('coarsens multi-node hourly ranges beyond 31 inclusive days', () => {
    expect(trafficNodeBucket('hour', '2026-01-01', '2026-01-31')).toBe('hour')
    expect(trafficNodeBucket('hour', '2026-01-01', '2026-02-01')).toBe('day')
    expect(trafficNodeBucket('minute', '2026-01-01', '2026-02-01')).toBe('day')
  })
  it('preserves supported single-node hour and explicit day requests', () => {
    expect(trafficNodeBucket('hour', '2026-01-01', '2026-12-31', true)).toBe('hour')
    expect(trafficNodeBucket('day', '2026-01-01', '2026-01-01')).toBe('day')
  })
})
