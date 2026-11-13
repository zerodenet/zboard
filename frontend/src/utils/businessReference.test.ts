import { describe, expect, it } from 'vitest'
import { orderReference, ticketReference, parseBusinessReference } from './businessReference'
describe('public business references', () => {
  it('uses the same stable value for old and new records', () => {
    expect(orderReference({ id: 48 })).toBe('DD0000000048')
    expect(orderReference({ id: 48, order_no: 'DD0000000048' })).toBe('DD0000000048')
    expect(ticketReference(12)).toBe('TK0000000012')
    expect(orderReference(10000000001)).toBe('DD10000000001')
    expect(orderReference(Number.MAX_SAFE_INTEGER + 1)).toBe('-')
  })
  it('allows pasting a ticket number into financial forms and rejects unsafe values', () => {
    expect(parseBusinessReference(' TK0000000012 ', 'T')).toBe(12)
    expect(parseBusinessReference('t0000000012', 'T')).toBe(12)
    expect(parseBusinessReference('12', 'T')).toBe(12)
    for (const value of ['DD0000000012', 'T0', '-12', '12.5', '1e3', 'T12x', 'TK9007199254740992', '+12']) expect(parseBusinessReference(value, 'T')).toBeUndefined()
  })
  it('shows creation seconds and safe prefixes, independently of the browser timezone', () => {
    const created_at = '2026-11-13T09:45:30.000Z'
    const order = orderReference({id:48,created_at})
    const ticket = ticketReference({id:12,created_at})
    expect(order).toBe('DD20261113174530000048')
    expect(ticket).toBe('TK20261113174530000012')
    expect(parseBusinessReference(order,'O')).toBe(48)
    expect(parseBusinessReference(ticket,'T')).toBe(12)
    expect(parseBusinessReference(ticket,'O')).toBeUndefined()
    expect(parseBusinessReference('DD20260230080000000048','O')).toBeUndefined()
  })

})
