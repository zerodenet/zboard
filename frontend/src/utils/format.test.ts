import { describe, expect, it } from 'vitest'
import { formatBytes, formatCompactDateTime, formatCurrency, formatDateTime, formatNumber, formatRelativeTime, formatSignedBytes, formatUnknownValue } from './format'

describe('formatters', () => {
  it('keeps zero distinct from missing and invalid numeric values', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(null)).toBe('—')
    expect(formatBytes(Number.NaN)).toBe('—')
    expect(formatBytes(-1024)).toBe('-1.00 KB')
    expect(formatBytes(438)).toBe('438 B')
    expect(formatSignedBytes(438)).toBe('+438 B')
    expect(formatSignedBytes(-438)).toBe('-438 B')
    expect(formatNumber(0)).toBe('0')
    expect(formatNumber(null)).toBe('—')
    expect(formatCurrency(0)).toContain('0.00')
    expect(formatCurrency(undefined)).toBe('—')
  })

  it('never echoes invalid or missing timestamps', () => {
    expect(formatDateTime()).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('无效时间')
    expect(formatRelativeTime('not-a-date')).toBe('无效时间')
    expect(formatCompactDateTime('not-a-date')).toBe('无效时间')
  })

  it('uses relative labels for recent timestamps', () => {
    const now = Date.UTC(2026, 6, 22, 10, 0, 0)
    expect(formatRelativeTime(new Date(now - 60_000).toISOString(), now)).toContain('1分钟')
    expect(formatCompactDateTime(new Date(now - 60_000).toISOString(), now)).toContain('1分钟')
  })

  it('labels unknown enum values without losing the source value', () => {
    expect(formatUnknownValue('状态', 'future_state')).toBe('未知状态（future_state）')
    expect(formatUnknownValue('状态', '')).toBe('未知状态（空）')
  })
})
