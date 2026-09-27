import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { finishNavigationProgress, navigationProgress, navigationProgressGeneration, navigationProgressVisible, startNavigationProgress } from './navigationProgress'

describe('navigation progress', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    finishNavigationProgress()
    vi.advanceTimersByTime(2000)
    vi.useRealTimers()
  })

  it('waits for the minimum display time before completing and hiding', () => {
    startNavigationProgress()
    expect(navigationProgressVisible.value).toBe(true)
    expect(navigationProgress.value).toBe(8)
    vi.advanceTimersByTime(360)
    expect(navigationProgress.value).toBeGreaterThan(8)
    expect(navigationProgress.value).toBeLessThan(100)
    finishNavigationProgress()
    expect(navigationProgress.value).toBeLessThan(100)
    vi.advanceTimersByTime(119)
    expect(navigationProgress.value).toBeLessThan(100)
    vi.advanceTimersByTime(1)
    expect(navigationProgress.value).toBe(100)
    vi.advanceTimersByTime(219)
    expect(navigationProgressVisible.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(navigationProgressVisible.value).toBe(false)
    expect(navigationProgress.value).toBe(0)
  })

  it('keeps progress active through redirects and a new navigation', () => {
    startNavigationProgress()
    vi.advanceTimersByTime(180)
    const advanced = navigationProgress.value
    const firstGeneration = navigationProgressGeneration()
    startNavigationProgress()
    expect(navigationProgress.value).toBe(advanced)
    finishNavigationProgress(firstGeneration)
    expect(navigationProgress.value).toBe(advanced)
    finishNavigationProgress()
    startNavigationProgress()
    vi.advanceTimersByTime(240)
    expect(navigationProgressVisible.value).toBe(true)
    expect(navigationProgress.value).toBeGreaterThan(8)
  })
})
