export type SortDirection = 'asc' | 'desc'
export type TableDensity = 'compact' | 'comfortable'

export function resolveSortField<T extends string>(value: unknown, allowed: ReadonlySet<T>, fallback: T): T {
  return typeof value === 'string' && allowed.has(value as T) ? value as T : fallback
}

export function resolveSortDirection(value: unknown, fallback: SortDirection): SortDirection {
  return value === 'asc' || value === 'desc' ? value : fallback
}

export function resolveTableDensity(value: unknown): TableDensity {
  return value === 'comfortable' ? 'comfortable' : 'compact'
}

export function nextSortDirection(currentField: string, nextField: string, currentDirection: SortDirection, defaultDirection: SortDirection): SortDirection {
  return currentField === nextField ? (currentDirection === 'asc' ? 'desc' : 'asc') : defaultDirection
}
