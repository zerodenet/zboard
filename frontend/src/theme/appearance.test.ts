import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { join } from 'node:path'
import { createAppearance, appearanceStorageKey } from './appearance'

const controllers: Array<ReturnType<typeof createAppearance>> = []
let media: EventTarget & { matches: boolean }
beforeEach(() => {
  localStorage.clear()
  media = Object.assign(new EventTarget(), { matches: false, media: '(prefers-color-scheme: dark)' })
  vi.spyOn(window, 'matchMedia').mockReturnValue(media as unknown as MediaQueryList)
})
afterEach(() => {
  controllers.splice(0).forEach(controller => controller.dispose())
  document.documentElement.classList.remove('dark')
  document.documentElement.style.removeProperty('color-scheme')
  delete document.documentElement.dataset.appearance
  vi.restoreAllMocks()
})
function start() { const controller = createAppearance(window); controllers.push(controller); return controller }
function system(dark: boolean) {
  media.matches = dark
  media.dispatchEvent(Object.assign(new Event('change'), { matches: dark }))
}

describe('appearance preference', () => {
  it('follows system changes until an explicit choice and restores following immediately', () => {
    const controller = start()
    expect(controller.preference.value).toBe('system')
    system(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    controller.setPreference('light')
    system(false); system(true)
    expect(controller.resolved.value).toBe('light')
    expect(document.documentElement.style.colorScheme).toBe('light')
    expect(localStorage.getItem(appearanceStorageKey)).toBe('light')
    controller.setPreference('system')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('restores a saved selection across reloads', () => {
    localStorage.setItem(appearanceStorageKey, 'dark')
    const controller = start()
    expect(controller.resolved.value).toBe('dark')
    expect(document.documentElement.dataset.appearance).toBe('dark')
    controller.setPreference('light')
    expect(start().resolved.value).toBe('light')
  })

  it('synchronizes tabs without writing the received preference back', () => {
    const controller = start()
    window.dispatchEvent(new StorageEvent('storage', { key: appearanceStorageKey, newValue: 'dark' }))
    expect(controller.resolved.value).toBe('dark')
    expect(localStorage.getItem(appearanceStorageKey)).toBeNull()
    window.dispatchEvent(new StorageEvent('storage', { key: 'auth.token', newValue: null }))
    expect(controller.resolved.value).toBe('dark')
    window.dispatchEvent(new StorageEvent('storage', { key: null, newValue: null }))
    expect(controller.preference.value).toBe('system')
    expect(controller.resolved.value).toBe('light')
  })

  it('still switches when browser storage is denied and stops listening when disposed', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied') })
    const controller = start()
    expect(() => controller.setPreference('dark')).not.toThrow()
    expect(controller.resolved.value).toBe('dark')
    controller.setPreference('system'); controller.dispose(); system(true)
    expect(controller.resolved.value).toBe('light')
  })

  it.each([
    ['dark', false, true], ['light', true, false], ['system', true, true],
    ['system', false, false], [null, true, true], ['invalid', true, true],
  ])('applies %s before the app loads, consistent with the runtime controller', (stored, darkSystem, expected) => {
    const html = readFileSync(join(import.meta.dirname, '../../index.html'), 'utf8')
    const script = html.match(/<script>([\s\S]*?)<\/script>/)![1]
    media.matches = darkSystem as boolean
    if (stored) localStorage.setItem(appearanceStorageKey, stored as string)
    runInNewContext(script, { localStorage, window, document })
    expect(document.documentElement.classList.contains('dark')).toBe(expected)
    expect(start().resolved.value === 'dark').toBe(expected)
  })
})
