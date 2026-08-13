import { beforeEach, describe, expect, it } from 'vitest'
import { expireAuthSession, isAuthSessionExpired, resetAuthSessionExpired } from './authSession'

beforeEach(() => resetAuthSessionExpired())

describe('auth session expiry', () => {
  it('marks an expired session only once until navigation resets it', () => {
    expect(isAuthSessionExpired()).toBe(false)
    expect(expireAuthSession()).toBe(true)
    expect(isAuthSessionExpired()).toBe(true)
    expect(expireAuthSession()).toBe(false)

    resetAuthSessionExpired()
    expect(isAuthSessionExpired()).toBe(false)
    expect(expireAuthSession()).toBe(true)
  })
})
