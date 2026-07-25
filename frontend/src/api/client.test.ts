import { describe, expect, it } from 'vitest'
import { normalizePageResult } from './client'

describe('normalizePageResult', () => {
  it('prefers the canonical page envelope and preserves aggregates and facets', () => {
    const result = normalizePageResult<{ id: number }, { failed: number }, { status: string[] }>({
      items: [{ id: 1 }],
      page: { offset: 50, limit: 25, total: 5000, next_cursor: 'cursor-2', previous_cursor: 'cursor-1' },
      aggregates: { failed: 3 },
      facets: { status: ['active'] },
      total: 1,
    })
    expect(result.page).toEqual({ offset: 50, limit: 25, total: 5000, next_cursor: 'cursor-2', previous_cursor: 'cursor-1' })
    expect(result.total).toBe(5000)
    expect(result.aggregates.failed).toBe(3)
    expect(result.facets.status).toEqual(['active'])
  })

  it('normalizes the temporary flat compatibility response', () => {
    const result = normalizePageResult({ items: [{ id: 1 }], total: 1000, offset: 25, limit: 25 })
    expect(result.page).toEqual({ offset: 25, limit: 25, total: 1000, next_cursor: null, previous_cursor: null })
    expect(result.items).toHaveLength(1)
  })
})
