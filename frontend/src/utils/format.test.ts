import { afterEach, describe, expect, it } from 'vitest'
import { formatBytes, formatCompactDateTime, formatCurrency, formatDateTime, formatExactDateTime, formatNumber, formatRelativeTime, formatSignedBytes, formatUnknownValue } from './format'
import { setDisplayTimeZone } from './timeZone'

afterEach(() => setDisplayTimeZone('UTC'))

describe('formatters', () => {
  it('keeps zero distinct from missing and invalid numeric values', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(null)).toBe('—')
    expect(formatBytes(Number.NaN)).toBe('—')
    expect(formatBytes(-1024)).toBe('-1.00 KB')
    expect(formatSignedBytes(438)).toBe('+438 B')
    expect(formatSignedBytes(-438)).toBe('-438 B')
    expect(formatNumber(0)).toBe('0')
    expect(formatNumber(null)).toBe('—')
    expect(formatCurrency(0)).toContain('0.00')
    expect(formatCurrency(undefined)).toBe('—')
  })

  it('does not round remaining quota back to the full allowance', () => {
    const gibibyte = 1024 ** 3
    const mebibyte = 1024 ** 2
    const remaining = 100 * gibibyte - Math.round(3.23 * mebibyte)

    expect(formatBytes(100 * gibibyte)).toBe('100 GB')
    expect(formatBytes(remaining)).toBe('99.997 GB')
    expect(formatBytes(2 * mebibyte - 100)).toBe('1.9999 MB')
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

  it('renders existing absolute timestamps in the configured system timezone', () => {
    const historicalOrderTime = '2026-08-18T02:00:00Z'
    setDisplayTimeZone('UTC')
    expect(formatDateTime(historicalOrderTime)).toContain('02:00')

    setDisplayTimeZone('Asia/Shanghai')
    expect(formatDateTime(historicalOrderTime)).toContain('10:00')
    expect(formatExactDateTime(historicalOrderTime)).toContain('10:00:00')
  })

  it('falls back to UTC for an invalid runtime timezone', () => {
    expect(setDisplayTimeZone('Definitely/Invalid')).toBe('UTC')
    expect(formatDateTime('2026-08-18T02:00:00Z')).toContain('02:00')
  })

  it('labels unknown enum values without losing the source value', () => {
    expect(formatUnknownValue('状态', 'future_state')).toBe('未知状态（future_state）')
    expect(formatUnknownValue('状态', '')).toBe('未知状态（空）')
  })
})
