export const AUTH_SESSION_EXPIRED_EVENT = 'zboard:auth-session-expired'

let authSessionExpired = false

export function expireAuthSession() {
  if (authSessionExpired) return false
  authSessionExpired = true
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(AUTH_SESSION_EXPIRED_EVENT))
  return true
}

export function isAuthSessionExpired() {
  return authSessionExpired
}

export function resetAuthSessionExpired() {
  authSessionExpired = false
}
