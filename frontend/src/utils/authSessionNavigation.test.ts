import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installAuthSessionNavigation } from './authSessionNavigation'
import { expireAuthSession, isAuthSessionExpired, resetAuthSessionExpired } from './authSession'
const cleanup: (() => void)[] = []
beforeEach(resetAuthSessionExpired)
afterEach(() => cleanup.splice(0).forEach(dispose => dispose()))
async function setup(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/admin/nodes', component: {}, meta: { requiresAuth: true } },
    { path: '/account', component: {}, meta: { requiresAuth: true } },
    { path: '/login', component: {} },
    { path: '/', component: {} },
  ] })
  await router.push(path)
  const clear = vi.fn()
  cleanup.push(installAuthSessionNavigation(router, clear))
  return { router, clear }
}
describe('expired session navigation', () => {
  it('replaces a protected page with login and retains its query and hash', async () => {
    const { router, clear } = await setup('/admin/nodes?page=3#details')
    const dirtyGuard = vi.fn(() => isAuthSessionExpired())
    router.beforeEach(dirtyGuard)
    expireAuthSession()
    expireAuthSession()
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe('/login'))
    expect(router.currentRoute.value.query).toEqual({ redirect: '/admin/nodes?page=3#details', reason: 'session-expired' })
    expect(clear).toHaveBeenCalledOnce()
    expect(dirtyGuard).toHaveReturnedWith(true)
    expect(isAuthSessionExpired()).toBe(false)
  })
  it('keeps public pages in place when a remembered login expires', async () => {
    const { router, clear } = await setup('/')
    expireAuthSession()
    expect(clear).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/')
    expect(isAuthSessionExpired()).toBe(false)
  })
})
