import { describe, expect, it } from 'vitest'
import { createRequestGuard } from './request'

describe('request guard', () => {
  it('rejects a late response after a newer request starts or the owner invalidates it', () => {
    const guard = createRequestGuard()
    const first = guard.begin()
    const second = guard.begin()
    expect(guard.isCurrent(first)).toBe(false)
    expect(guard.isCurrent(second)).toBe(true)
    guard.invalidate()
    expect(guard.isCurrent(second)).toBe(false)
  })
})
