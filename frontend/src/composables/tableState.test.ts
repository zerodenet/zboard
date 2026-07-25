import { describe, expect, it } from 'vitest'
import { nextSortDirection, resolveSortDirection, resolveSortField, resolveTableDensity } from './tableState'

describe('tableState', () => {
  it('accepts only URL values declared by the page contract', () => {
    const allowed = new Set(['id', 'name'] as const)
    expect(resolveSortField('name', allowed, 'id')).toBe('name')
    expect(resolveSortField('DROP TABLE', allowed, 'id')).toBe('id')
    expect(resolveSortDirection('desc', 'asc')).toBe('desc')
    expect(resolveSortDirection('sideways', 'asc')).toBe('asc')
    expect(resolveTableDensity('comfortable')).toBe('comfortable')
    expect(resolveTableDensity('huge')).toBe('compact')
  })

  it('toggles the active field and applies a field default for a new sort', () => {
    expect(nextSortDirection('name', 'name', 'asc', 'asc')).toBe('desc')
    expect(nextSortDirection('id', 'name', 'desc', 'asc')).toBe('asc')
  })
})
