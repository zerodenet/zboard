import { AxiosError, type AxiosAdapter } from 'axios'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, clearAuthToken, getAuthToken, setAuthToken } from './client'
import { AUTH_SESSION_EXPIRED_EVENT, isAuthSessionExpired, resetAuthSessionExpired } from '../utils/authSession'
const rejectStatus = (status: number, data: unknown = ''): AxiosAdapter => async config => {
  throw new AxiosError('request failed', undefined, config, undefined, { status, data, config, headers: {}, statusText: '' })
}
const ok: AxiosAdapter = async config => ({ status: 200, data: {}, config, headers: {}, statusText: '' })
beforeEach(() => {
  clearAuthToken()
  resetAuthSessionExpired()
  api.defaults.adapter = ok
})
describe('API login lifecycle', () => {
  it('expires the current session once for concurrent bare HTTP 401 failures', async () => {
    setAuthToken('current')
    const listener = vi.fn()
    window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, listener)
    try {
      await Promise.allSettled([api.get('/navigation', { adapter: rejectStatus(401) }), api.get('/admin/users', { adapter: rejectStatus(401) })])
      expect(isAuthSessionExpired()).toBe(true)
      expect(listener).toHaveBeenCalledOnce()
    } finally { window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, listener) }
  })
  it('ignores a late 401 from the previous login', async () => {
    setAuthToken('old')
    await api.get('/navigation', { adapter: async config => {
      setAuthToken('new')
      return rejectStatus(401)(config)
    } }).catch(() => undefined)
    expect(isAuthSessionExpired()).toBe(false)
    expect(getAuthToken()).toBe('new')
  })
  it('keeps login for forbidden and network failures', async () => {
    setAuthToken('current')
    await api.get('/navigation', { adapter: rejectStatus(403) }).catch(() => undefined)
    await api.get('/navigation', { adapter: async () => { throw new Error('offline') } }).catch(() => undefined)
    expect(isAuthSessionExpired()).toBe(false)
  })
  it('does not expire a login for wrong login credentials or an anonymous request', async () => {
    await api.get('/navigation', { adapter: rejectStatus(401) }).catch(() => undefined)
    setAuthToken('current')
    await api.post('/auth/login', {}, { adapter: rejectStatus(401) }).catch(() => undefined)
    expect(isAuthSessionExpired()).toBe(false)
  })
  it('checks the main session when a plugin proof fails', async () => {
    setAuthToken('current')
    const verification = vi.fn(ok)
    api.defaults.adapter = verification
    await api.post('/plugin-ui/bridge', {}, { headers: { 'X-Plugin-Session': 'expired-proof' }, adapter: rejectStatus(401, { code: 401, error: { code: 'unauthenticated' } }) }).catch(() => undefined)
    await vi.waitFor(() => expect(verification).toHaveBeenCalledOnce())
    expect(verification.mock.calls[0]?.[0].url).toBe('/auth/me')
    expect(isAuthSessionExpired()).toBe(false)
  })
  it('expires the main login if verification also returns 401', async () => {
    setAuthToken('current')
    api.defaults.adapter = rejectStatus(401)
    await api.post('/account/security/confirm-password', {}).catch(() => undefined)
    await vi.waitFor(() => expect(isAuthSessionExpired()).toBe(true))
  })
})
