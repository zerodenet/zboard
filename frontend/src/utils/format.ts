function byteFractionDigits(value: number): number {
  let digits = value >= 100 ? 0 : value >= 10 ? 1 : 2
  if (Number.isInteger(value)) return digits

  const nearestWhole = Math.round(value)
  while (digits < 4 && Number(value.toFixed(digits)) === nearestWhole) digits += 1
  return digits
}

export function formatBytes(bytes: number | undefined | null): string {
  if (bytes === null || bytes === undefined) return '—'
  const value = Number(bytes)
  if (!Number.isFinite(value)) return '—'
  if (value === 0) return '0 B'
  if (value < 0) return `-${formatBytes(Math.abs(value))}`
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const normalized = value / 1024 ** index
  return `${normalized.toFixed(byteFractionDigits(normalized))} ${units[index]}`
}

export function formatSignedBytes(bytes: number | undefined | null): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(Number(bytes))) return '—'
  const value = Number(bytes)
  return `${value > 0 ? '+' : ''}${formatBytes(value)}`
}

export function formatCurrency(cents: number | undefined | null, currency = 'CNY') {
  if (cents === null || cents === undefined || !Number.isFinite(Number(cents))) return '—'
  return new Intl.NumberFormat('zh-CN', { style: 'currency', currency, minimumFractionDigits: 2 }).format(Number(cents) / 100)
}

export function formatDateTime(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '无效时间'
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
}

export function formatExactDateTime(value?: string | null) {
  if (!value) return '未记录时间'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '无效时间'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short',
  }).format(date)
}

export function formatRelativeTime(value?: string | null, now = Date.now()) {
  if (!value) return '未记录'
  const date = new Date(value)
  const timestamp = date.getTime()
  if (Number.isNaN(timestamp)) return '无效时间'

  const difference = timestamp - now
  const absolute = Math.abs(difference)
  const formatter = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['year', 365 * 24 * 60 * 60 * 1000],
    ['month', 30 * 24 * 60 * 60 * 1000],
    ['day', 24 * 60 * 60 * 1000],
    ['hour', 60 * 60 * 1000],
    ['minute', 60 * 1000],
  ]
  const [unit, milliseconds] = units.find(([, threshold]) => absolute >= threshold) || ['second', 1000]
  return formatter.format(Math.round(difference / milliseconds), unit)
}

export function formatCompactDateTime(value?: string | null, now = Date.now()) {
  if (!value) return '未记录'
  const date = new Date(value)
  const timestamp = date.getTime()
  if (Number.isNaN(timestamp)) return '无效时间'
  if (Math.abs(timestamp - now) < 7 * 24 * 60 * 60 * 1000) return formatRelativeTime(value, now)
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
}

export function formatNumber(value: number | undefined | null) {
  if (value === null || value === undefined || !Number.isFinite(Number(value))) return '—'
  return new Intl.NumberFormat('zh-CN').format(Number(value))
}

export function formatUnknownValue(kind: string, value: unknown) {
  const raw = value === null || value === undefined || value === '' ? '空' : String(value)
  return `未知${kind}（${raw}）`
}

export function shortVersion(version?: string | null) {
  if (!version) return '未知版本'
  const normalized = version.replace(/^vv/, 'v')
  const match = normalized.match(/^v?\d+\.\d+\.\d+(?:-[^.@]+)?/)
  return match?.[0] || normalized.split('@')[0]
}

export function daysRemaining(endAt?: string | null) {
  if (!endAt) return '未设置'
  const remaining = new Date(endAt).getTime() - Date.now()
  if (!Number.isFinite(remaining)) return '未知'
  if (remaining <= 0) return '已到期'
  return `${Math.ceil(remaining / 86400000)} 天`
}
