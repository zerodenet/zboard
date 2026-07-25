import { describe, expect, it } from 'vitest'
import { defaultHistoryRange, isHistoryDate, resolveHistoryRange } from './historyState'

describe('historyState', () => {
  const now = new Date('2026-07-22T12:00:00.000Z')

  it('builds an inclusive UTC date range', () => {
    expect(defaultHistoryRange(7, now)).toEqual({ from: '2026-07-16', to: '2026-07-22' })
    expect(defaultHistoryRange(30, now)).toEqual({ from: '2026-06-23', to: '2026-07-22' })
  })

  it('keeps valid URL dates and rejects invalid or oversized ranges', () => {
    expect(resolveHistoryRange({ from: '2026-07-01', to: '2026-07-03' }, 30, now)).toEqual({ from: '2026-07-01', to: '2026-07-03' })
    expect(resolveHistoryRange({ from: '2026-99-01', to: '2026-07-03' }, 30, now)).toEqual({ from: '2026-06-23', to: '2026-07-03' })
    expect(resolveHistoryRange({ from: '2025-01-01', to: '2026-07-03' }, 30, now)).toEqual({ from: '2026-06-23', to: '2026-07-22' })
    expect(isHistoryDate('2026-02-29')).toBe(false)
  })
})
