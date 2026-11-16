import { expect, it } from 'vitest'
import { dateTimeInputISO, dateTimeInputValue } from './dateTimeInput'
it('uses the displayed zone and preserves the selected minute', () => {
  expect(dateTimeInputValue('2030-04-05T03:04:59.123Z', 'Asia/Shanghai')).toBe('2030-04-05T11:04')
  expect(dateTimeInputISO('2030-04-05T11:04', 'Asia/Shanghai')).toBe('2030-04-05T03:04:00.000Z')
  expect(dateTimeInputISO('2030-04-05T03:04', 'UTC')).toBe('2030-04-05T03:04:00.000Z')
  expect(dateTimeInputISO('2030-04-05T08:49', 'Asia/Kathmandu')).toBe('2030-04-05T03:04:00.000Z')
})
it('rejects invalid dates and nonexistent or ambiguous DST times', () => {
  expect(dateTimeInputISO('2030-02-30T09:00', 'UTC')).toBeUndefined()
  expect(dateTimeInputISO('2026-03-08T02:30', 'America/New_York')).toBeUndefined()
  expect(dateTimeInputISO('2026-11-01T01:30', 'America/New_York')).toBeUndefined()
  expect(dateTimeInputISO('', 'UTC')).toBeUndefined()
})
