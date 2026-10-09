import type { Router } from 'vue-router'
import { AUTH_SESSION_EXPIRED_EVENT, resetAuthSessionExpired } from './authSession'

export function installAuthSessionNavigation(router: Router, clearAuth: () => void) {
  function onExpired() {
    const current = router.currentRoute.value
    clearAuth()
    if (!current.meta.requiresAuth) {
      resetAuthSessionExpired()
      return
    }
    // Keep expiry active through leave guards, including dirty forms.
    void router.replace({ path: '/login', query: { redirect: current.fullPath, reason: 'session-expired' } })
      .finally(resetAuthSessionExpired)
  }
  window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, onExpired)
  return () => window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, onExpired)
}
