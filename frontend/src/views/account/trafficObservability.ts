import {
  fetchAccountTrafficRecordsPage,
  type TrafficRecordSummary,
} from '../../api/client'

const PAGE_SIZE = 100
export const TRAFFIC_OBSERVABILITY_RECORD_LIMIT = 5000

export interface TrafficObservabilityRecord extends TrafficRecordSummary {
  /**
   * Optional forward-compatible field. The current kernel does not report it yet.
   * When available, this value must represent the highest concurrent connection
   * count observed during this record's sampling window.
   */
  peak_connections?: number | null
}

export interface TrafficObservabilityPoint {
  date: string
  label: string
  upload_bytes: number
  download_bytes: number
  used_bytes: number
  peak_connections: number | null
}

export interface TrafficObservabilityResult {
  points: TrafficObservabilityPoint[]
  record_count: number
  connection_sample_count: number
  peak_connections: number | null
  truncated: boolean
}

export interface TrafficObservabilityQuery {
  subscriptionId?: number
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
  }
}

function parseDateKey(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const date = new Date(`${value}T00:00:00.000Z`)
  return Number.isNaN(date.getTime()) ? null : date
}

function formatDateKey(date: Date): string {
  return date.toISOString().slice(0, 10)
}

function formatDateLabel(value: string): string {
  const date = parseDateKey(value)
  if (!date) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    timeZone: 'UTC',
  }).format(date)
}

function finiteNonNegative(value: unknown): number | null {
  if (value === null || value === undefined || value === '') return null
  const number = Number(value)
  return Number.isFinite(number) && number >= 0 ? number : null
}

export function resolvePeakConnections(record: TrafficObservabilityRecord): number | null {
  return finiteNonNegative(record.peak_connections)
}

function resolveRange(records: TrafficObservabilityRecord[], from: string, to: string): { from: string; to: string } | null {
  const requestedFrom = parseDateKey(from)
  const requestedTo = parseDateKey(to)
  if (requestedFrom && requestedTo && requestedFrom <= requestedTo) return { from, to }

  const recordDates = records
    .map(record => String(record.record_at || '').slice(0, 10))
    .filter(value => parseDateKey(value))
    .sort()

  if (!recordDates.length) return null
  return { from: recordDates[0], to: recordDates[recordDates.length - 1] }
}

export function aggregateTrafficObservability(
  records: TrafficObservabilityRecord[],
  from: string,
  to: string,
): TrafficObservabilityResult {
  const range = resolveRange(records, from, to)
  if (!range) return emptyTrafficObservabilityResult()

  const buckets = new Map<string, TrafficObservabilityPoint>()
  const start = parseDateKey(range.from)!
  const end = parseDateKey(range.to)!

  for (let cursor = new Date(start); cursor <= end; cursor.setUTCDate(cursor.getUTCDate() + 1)) {
    const date = formatDateKey(cursor)
    buckets.set(date, {
      date,
      label: formatDateLabel(date),
      upload_bytes: 0,
      download_bytes: 0,
      used_bytes: 0,
      peak_connections: null,
    })
  }

  let connectionSampleCount = 0
  let overallPeakConnections: number | null = null

  for (const record of records) {
    const date = String(record.record_at || '').slice(0, 10)
    const bucket = buckets.get(date)
    if (!bucket) continue

    bucket.upload_bytes += finiteNonNegative(record.upload_bytes) || 0
    bucket.download_bytes += finiteNonNegative(record.download_bytes) || 0
    bucket.used_bytes += finiteNonNegative(record.used_bytes) || 0

    const peakConnections = resolvePeakConnections(record)
    if (peakConnections !== null) {
      connectionSampleCount += 1
      bucket.peak_connections = Math.max(bucket.peak_connections ?? 0, peakConnections)
      overallPeakConnections = Math.max(overallPeakConnections ?? 0, peakConnections)
    }
  }

  return {
    points: Array.from(buckets.values()),
    record_count: records.length,
    connection_sample_count: connectionSampleCount,
    peak_connections: overallPeakConnections,
    truncated: false,
  }
}

export async function fetchTrafficObservability(query: TrafficObservabilityQuery): Promise<TrafficObservabilityResult> {
  const records = new Map<number, TrafficObservabilityRecord>()
  const seenCursors = new Set<string>()
  let cursor: string | undefined
  let truncated = false

  while (records.size < TRAFFIC_OBSERVABILITY_RECORD_LIMIT) {
    const page = await fetchAccountTrafficRecordsPage({
      subscriptionId: query.subscriptionId,
      cursor,
      from: query.from,
      to: query.to,
      limit: PAGE_SIZE,
    }, { signal: query.signal })

    for (const item of page.items as TrafficObservabilityRecord[]) {
      records.set(item.id, item)
      if (records.size >= TRAFFIC_OBSERVABILITY_RECORD_LIMIT) break
    }

    const nextCursor = page.page.next_cursor || undefined
    if (!nextCursor) break
    if (records.size >= TRAFFIC_OBSERVABILITY_RECORD_LIMIT) {
      truncated = true
      break
    }
    if (seenCursors.has(nextCursor)) break

    seenCursors.add(nextCursor)
    cursor = nextCursor
  }

  return {
    ...aggregateTrafficObservability(Array.from(records.values()), query.from, query.to),
    truncated,
  }
}
