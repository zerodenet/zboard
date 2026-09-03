/** Preserve URL/input text until validation; NaN must never mean no filter. */
export type QueryID = number | string

export function optionalQueryID(value: QueryID | undefined, label: string): number | undefined {
  if (value === undefined || value === '') return undefined
  const raw = String(value).trim()
  const id = Number(raw)
  if (!/^\d+$/.test(raw) || !Number.isSafeInteger(id) || id <= 0) {
    throw new Error(`${label}必须是有效的正整数编号。`)
  }
  return id
}

export function appendQueryID(query: URLSearchParams, key: string, value: QueryID | undefined) {
  const id = optionalQueryID(value, key)
  if (id !== undefined) query.set(key, String(id))
}
