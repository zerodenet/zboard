/** A broken page envelope is a read failure, never a successful empty list. */
export function requirePageResponse(data: unknown, allowUnknownTotal = false): any {
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('分页响应不完整。')
  const source = data as Record<string, any>
  const total = source.page && typeof source.page === 'object' ? source.page.total : source.total
  if (!Array.isArray(source.items) || !(allowUnknownTotal && total === null ||
    typeof total === 'number' && Number.isSafeInteger(total) && total >= 0)) {
    throw new Error('分页响应不完整。')
  }
  return source
}
