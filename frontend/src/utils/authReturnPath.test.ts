import { beforeEach, describe, expect, it, vi } from 'vitest'
import { consumeAuthReturnPath, rememberAuthReturnPath } from './authReturnPath'

beforeEach(() => sessionStorage.clear())

describe('external authentication return path', () => {
  it('restores the same account path once, with filters and anchor', () => {
    rememberAuthReturnPath('/admin/nodes?page=3#details', 1)
    expect(consumeAuthReturnPath(1, true)).toBe('/admin/nodes?page=3#details')
    expect(consumeAuthReturnPath(1, true)).toBe('')
  })
  it('discards another account context or a revoked admin path', () => {
    rememberAuthReturnPath('/account/orders/42', 1)
    expect(consumeAuthReturnPath(2, false)).toBe('')
    rememberAuthReturnPath('/admin/nodes', 1)
    expect(consumeAuthReturnPath(1, false)).toBe('')
  })
  it.each(['https://example.test', '//example.test/account', '/login', '/accounting', '/administrator'])('rejects an unrelated or external path %s', path => {
    rememberAuthReturnPath(path, 1)
    expect(consumeAuthReturnPath(1, true)).toBe('')
  })
  it('expires old hints and handles unavailable browser storage', () => {
    const clock = vi.spyOn(Date, 'now').mockReturnValue(1_000)
    try {
      rememberAuthReturnPath('/account/subscription', 1)
      clock.mockReturnValue(1_000 + 11 * 60_000)
      expect(consumeAuthReturnPath(1, false)).toBe('')
    } finally { clock.mockRestore() }
    const write = vi.spyOn(sessionStorage, 'setItem').mockImplementation(() => { throw new Error('disabled') })
    try {
      expect(() => rememberAuthReturnPath('/account', 1)).not.toThrow()
      expect(consumeAuthReturnPath(1, false)).toBe('')
    } finally { write.mockRestore() }
  })
})
