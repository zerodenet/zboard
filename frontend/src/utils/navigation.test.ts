import { describe, expect, it } from 'vitest'
import { normalizeAdminReturnTo, preserveAdminReturnTo, withAdminReturnTo } from './navigation'

describe('admin return context', () => {
  it('accepts bounded internal admin paths and preserves list state', () => {
    const path = '/admin/users?q=member&page=3&user=42'
    expect(normalizeAdminReturnTo(path)).toBe(path)
    expect(preserveAdminReturnTo(path)).toEqual({ return_to: path })
    expect(withAdminReturnTo('/admin/subscriptions', path, { user_id: '42' })).toEqual({
      path: '/admin/subscriptions',
      query: { user_id: '42', return_to: path },
    })
  })

  it('rejects external, normalized-outside-admin and control-character targets', () => {
    expect(normalizeAdminReturnTo('https://example.com/admin/users')).toBe('')
    expect(normalizeAdminReturnTo('/admin/../../login')).toBe('')
    expect(normalizeAdminReturnTo('/account/orders')).toBe('')
    expect(normalizeAdminReturnTo('/admin/users\n?user=1')).toBe('')
  })

  it('uses the first query value and drops fragments', () => {
    expect(normalizeAdminReturnTo(['/admin/orders?user_id=4#detail', '/admin/users'])).toBe('/admin/orders?user_id=4')
  })
})
