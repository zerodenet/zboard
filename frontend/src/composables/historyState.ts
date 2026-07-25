export interface HistoryRange {
  from: string
  to: string
}
function dateString(value: Date) {
  return value.toISOString().slice(0, 10)
}

export function isHistoryDate(value: unknown): value is string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const parsed = new Date(`${value}T00:00:00.000Z`)
  return !Number.isNaN(parsed.getTime()) && dateString(parsed) === value
}

export function defaultHistoryRange(days: number, now = new Date()): HistoryRange {
  const to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()))
  const from = new Date(to)
  from.setUTCDate(from.getUTCDate() - Math.max(0, days - 1))
  return { from: dateString(from), to: dateString(to) }
}

export function resolveHistoryRange(query: Record<string, unknown>, days: number, now = new Date()): HistoryRange {
  const fallback = defaultHistoryRange(days, now)
  const from = isHistoryDate(query.from) ? query.from : fallback.from
  const to = isHistoryDate(query.to) ? query.to : fallback.to
  const span = (Date.parse(`${to}T00:00:00.000Z`) - Date.parse(`${from}T00:00:00.000Z`)) / 86400000
  if (span < 0 || span >= 366) return fallback
  return { from, to }
}
