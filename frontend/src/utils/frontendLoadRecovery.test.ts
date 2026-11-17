import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { frontendLoadFailure, installFrontendLoadRecovery } from './frontendLoadRecovery'

const cleanup: (() => void)[] = []
afterEach(() => {
  cleanup.splice(0).forEach(dispose => dispose())
  frontendLoadFailure.value = null
})

function setup(failure: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: {} },
    { path: '/unavailable', component: () => Promise.reject(new Error(failure)) },
  ] })
  cleanup.push(installFrontendLoadRecovery(router))
  return router
}

describe('frontend load recovery', () => {
  it.each([
    'Failed to fetch dynamically imported module: /assets/Home-old.js',
    'Importing a module script failed.',
    'error loading dynamically imported module',
    'Unable to preload CSS for /assets/Navigation-old.css',
    'Loading chunk 1 failed',
  ])('shows recovery when the initial route fails: %s', async message => {
    const router = setup(message)
    await expect(router.push('/unavailable')).rejects.toThrow(message)
    expect(router.currentRoute.value.matched).toHaveLength(0)
    expect(frontendLoadFailure.value).toBe('assets')
  })

  it('keeps the current route on failure and clears the notice on successful navigation', async () => {
    const router = setup('Failed to fetch dynamically imported module')
    await router.push('/')
    await expect(router.push('/unavailable')).rejects.toThrow()
    expect(router.currentRoute.value.path).toBe('/')
    expect(frontendLoadFailure.value).toBe('assets')
    await router.push('/?tab=overview')
    expect(frontendLoadFailure.value).toBeNull()
  })

  it('handles preload failures without suppressing the failed navigation or clearing login', () => {
    const router = setup('unused')
    const storageSpy = vi.spyOn(Storage.prototype, 'clear')
    const event = new Event('vite:preloadError', { cancelable: true })
    window.dispatchEvent(event)
    expect(frontendLoadFailure.value).toBe('assets')
    expect(event.defaultPrevented).toBe(false)
    expect(storageSpy).not.toHaveBeenCalled()
    expect(router.currentRoute.value.matched).toHaveLength(0)
  })

  it('also ends the boot wait for other navigation exceptions', async () => {
    const router = setup('navigation guard failed')
    await expect(router.push('/unavailable')).rejects.toThrow()
    expect(frontendLoadFailure.value).toBe('navigation')
  })
})
