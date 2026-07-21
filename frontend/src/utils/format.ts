export function formatBytes(bytes: number | undefined | null) {
  const value = Number(bytes) || 0
  if (value === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const normalized = value / 1024 ** index
  const digits = normalized >= 100 ? 0 : normalized >= 10 ? 1 : 2
  return `${normalized.toFixed(digits)} ${units[index]}`
}

export function formatCurrency(cents: number | undefined | null, currency = 'CNY') {
  return new Intl.NumberFormat('zh-CN', { style: 'currency', currency, minimumFractionDigits: 2 }).format((Number(cents) || 0) / 100)
}

export function formatDateTime(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
}

export function formatNumber(value: number | undefined | null) {
  return new Intl.NumberFormat('zh-CN').format(Number(value) || 0)
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
