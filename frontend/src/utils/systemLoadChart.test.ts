import { expect, it } from 'vitest'
import { networkCeiling, tooltipPosition, trendPaths } from './systemLoadChart'
import type { SystemPoint } from '../api/systemMetrics'
const sample = (at: string, cpu: number | null = 0): SystemPoint => ({ at, cpu_percent: cpu, memory_percent: 20, disk_percent: 6, rx_bytes_per_second: 1024, tx_bytes_per_second: 2048 })
it('uses a separate bandwidth scale with headroom, preserving real zero', () => {
 expect(networkCeiling([sample('2026-11-03T10:00:00Z')])).toBe(5120)
 expect(networkCeiling([])).toBe(1024)
})
it('keeps the inspection panel inside narrow and wide charts at both edges', () => {
 for (const width of [280, 400, 700]) for (const point of [0, .5, 1]) {
  const left = tooltipPosition(point, width)
  expect(left).toBeGreaterThanOrEqual(4); expect(left + 148).toBeLessThanOrEqual(width - 4)
 }
})
it('breaks both lines and fills across missing minutes and collection outages', () => {
 const start = Date.parse('2026-11-03T10:00:00Z'), end = start + 600000
 const points = [sample('2026-11-03T10:00:00Z'), sample('2026-11-03T10:01:00Z', null), sample('2026-11-03T10:02:00Z', 30), sample('2026-11-03T10:06:00Z', 40)]
 const paths = trendPaths(points, 'cpu_percent', start, end, 100, 60)
 expect(paths.line.match(/M/g)).toHaveLength(3)
 expect(paths.area.match(/Z/g)).toHaveLength(3)
 expect(paths.line).not.toContain('NaN')
})
