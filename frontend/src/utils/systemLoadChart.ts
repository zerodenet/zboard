import type { SystemPoint } from '../api/systemMetrics'

export type LoadField = Exclude<keyof SystemPoint, 'at'>
export const loadSeries: { key: LoadField; label: string; axis: 'percent' | 'network'; color: string }[] = [
  { key: 'cpu_percent', label: 'CPU', axis: 'percent', color: 'var(--load-cpu)' },
  { key: 'memory_percent', label: 'MEM', axis: 'percent', color: 'var(--load-memory)' },
  { key: 'disk_percent', label: 'DISK', axis: 'percent', color: 'var(--load-disk)' },
  { key: 'rx_bytes_per_second', label: '↓ IN', axis: 'network', color: 'var(--load-in)' },
  { key: 'tx_bytes_per_second', label: '↑ OUT', axis: 'network', color: 'var(--load-out)' },
]

export function networkCeiling(points: SystemPoint[]) {
  const maximum = Math.max(0, ...points.flatMap(p => [p.rx_bytes_per_second ?? 0, p.tx_bytes_per_second ?? 0]))
  if (maximum === 0) return 1024
  const magnitude = 1024 ** Math.max(0, Math.floor(Math.log(maximum) / Math.log(1024)))
  const step = [1, 2, 5, 10, 20, 50, 100, 200, 500, 1024].find(n => n * magnitude >= maximum * 1.05) ?? 2048
  return step * magnitude
}

export function trendPosition(at: string, start: number, end: number) {
  return Math.max(0, Math.min(1, (Date.parse(at) - start) / Math.max(1, end - start)))
}

// Every segment owns its fill baseline; missing observations and long gaps
// cannot be connected, interpolated or filled as if data had been collected.
export function trendPaths(points: SystemPoint[], key: LoadField, start: number, end: number, ceiling: number, resolutionSeconds: number) {
  const segments: string[][] = []
  let segment: string[] = [], previous = 0
  for (const point of points) {
    const time = Date.parse(point.at), value = point[key]
    if (value == null || !Number.isFinite(value) || !Number.isFinite(time) || time < start || time > end) {
      if (segment.length) segments.push(segment)
      segment = []; continue
    }
    if (segment.length && (time - previous > resolutionSeconds * 1500 || time <= previous)) {
      segments.push(segment); segment = []
    }
    const x = (trendPosition(point.at, start, end) * 1000).toFixed(2)
    const y = ((1 - Math.max(0, Math.min(1, value / ceiling))) * 180).toFixed(2)
    segment.push(`${x},${y}`); previous = time
  }
  if (segment.length) segments.push(segment)
  return {
    line: segments.map(s => `M${s.join(' L')}`).join(' '),
    area: segments.map(s => `M${s[0]!.split(',')[0]},180 L${s.join(' L')} L${s.at(-1)!.split(',')[0]},180 Z`).join(' '),
  }
}

export function tooltipPosition(point: number, width: number) {
  const tooltipWidth = 148, cursor = 32 + Math.max(0, width - 76) * point
  const preferred = cursor + 14 + tooltipWidth <= width - 4 ? cursor + 14 : cursor - tooltipWidth - 14
  return Math.max(4, Math.min(width - tooltipWidth - 4, preferred))
}
