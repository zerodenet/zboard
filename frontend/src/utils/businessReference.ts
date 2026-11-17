type ID = number | string | null | undefined
function reference(prefix: 'DD' | 'TK', id: ID, created?: string) {
  if (typeof id === 'number' && (!Number.isSafeInteger(id) || id <= 0)) return '-'
  const digits = String(id ?? '')
  if (!/^\d+$/.test(digits) || !/[1-9]/.test(digits)) return '-'
  const suffix = digits.replace(/^0+/, '')
  if (created) {
    const parsed = Date.parse(created)
    if (Number.isFinite(parsed)) {
      const local = new Date(parsed + 8 * 3600 * 1000).toISOString().slice(0, 19).replace(/\D/g, '')
      return `${prefix}${local}${suffix.padStart(6, '0')}`
    }
  }
  return `${prefix}${suffix.padStart(10, '0')}`
}
export function orderReference(value: { id: number; order_no?: string; created_at?: string } | ID): string {
  return typeof value === 'object' && value !== null ? value.order_no || reference('DD', value.id, value.created_at) : reference('DD', value)
}
export function ticketReference(value: { id: number; ticket_no?: string; created_at?: string } | ID): string {
  return typeof value === 'object' && value !== null ? value.ticket_no || reference('TK', value.id, value.created_at) : reference('TK', value)
}
export function parseBusinessReference(value: unknown, kind: 'O' | 'T'): number | undefined {
  if (typeof value !== 'string' && typeof value !== 'number') return
  let input = String(value).trim()
  const prefix = kind === 'O' ? 'DD' : 'TK'
  if (input.toUpperCase().startsWith(prefix)) {
    input = input.slice(prefix.length)
    if (input.length >= 20 && /^\d+$/.test(input)) {
      const date = `${input.slice(0,4)}-${input.slice(4,6)}-${input.slice(6,8)}T${input.slice(8,10)}:${input.slice(10,12)}:${input.slice(12,14)}Z`
      const parsed = Date.parse(date)
      if (Number.isFinite(parsed) && new Date(parsed).toISOString().slice(0,19) === date.slice(0,19)) input = input.slice(14)
    }
  } else if (input[0]?.toUpperCase() === kind) input = input.slice(1) // legacy links only
  if (!/^\d+$/.test(input)) return
  const id = Number(input)
  return Number.isSafeInteger(id) && id > 0 ? id : undefined
}
