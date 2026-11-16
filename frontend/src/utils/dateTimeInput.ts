import { getDisplayTimeZone } from './timeZone'

/** Native datetime-local controls use the displayed site zone, never the browser zone. */
export function dateTimeInputValue(value?: string | null, zone = getDisplayTimeZone()) {
  if (!value || !Number.isFinite(Date.parse(value))) return ''
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value))
  const values = Object.fromEntries(parts.map(part => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}T${values.hour}:${values.minute}`
}

export function dateTimeInputISO(value: string, zone = getDisplayTimeZone()): string | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value)
  if (!match) return
  const [, y, m, d, h, min] = match.map(Number)
  if (y < 1000 || y > 9999 || m < 1 || m > 12 || d < 1 || d > 31 || h > 23 || min > 59) return
  const wall = Date.UTC(y, m - 1, d, h, min)
  const offsets = new Set<number>()
  for (const delta of [-86400000, 0, 86400000]) {
    const sample = wall + delta
    const shown = dateTimeInputValue(new Date(sample).toISOString(), zone)
    const offset = Date.parse(`${shown}:00Z`) - sample
    if (Number.isFinite(offset)) offsets.add(offset)
  }
  const candidates = [...offsets].map(offset => wall - offset).filter(time => dateTimeInputValue(new Date(time).toISOString(), zone) === value)
  // A missing/ambiguous DST wall time is not safe to silently reinterpret.
  if (candidates.length !== 1) return
  const selected = new Date(candidates[0])
  if (selected.getUTCFullYear() > 9999 || selected.getUTCFullYear() < 1000) return
  return selected.toISOString()
}
