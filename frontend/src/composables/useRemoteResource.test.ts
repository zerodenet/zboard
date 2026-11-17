import { effectScope } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useRemoteResource } from './useRemoteResource'
import { AUTH_SESSION_RESTORED_EVENT, expireAuthSession, resetAuthSessionExpired } from '../utils/authSession'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

describe('useRemoteResource', () => {
  it('retains an expired read until recovery, retries that read, and leaves healthy editor data alone', async () => {
    const scope = effectScope()
    const fetch = vi.fn().mockResolvedValueOnce(42).mockRejectedValueOnce({ response: { status: 401 } }).mockResolvedValueOnce(43)
    const healthyFetch = vi.fn().mockResolvedValue(7)
    const [resource, healthy] = scope.run(() => [
      useRemoteResource({ initial: () => 0, fetch, errorMessage: 'expired' }),
      useRemoteResource({ initial: () => 0, fetch: healthyFetch, errorMessage: 'failed' }),
    ])!
    try {
      await resource.load(); await healthy.load()
      expireAuthSession()
      await resource.load()
      expect(resource.data.value).toBe(42)
      expect(resource.phase.value).toBe('unauthorized')
      resetAuthSessionExpired()
      window.dispatchEvent(new Event(AUTH_SESSION_RESTORED_EVENT))
      await vi.waitFor(() => expect(resource.data.value).toBe(43))
      expect(healthyFetch).toHaveBeenCalledOnce()
      expect(healthy.data.value).toBe(7)
    } finally { scope.stop(); resetAuthSessionExpired() }
    window.dispatchEvent(new Event(AUTH_SESSION_RESTORED_EVENT))
    expect(fetch).toHaveBeenCalledTimes(3)
  })
  it('classifies a late response from an invalidated credential as requiring identity recovery', async () => {
    const resource = useRemoteResource({ initial: () => 0, fetch: () => Promise.reject({ code: 'AUTH_SESSION_CHANGED' }), errorMessage: 'identity changed' })
    await resource.load()
    expect(resource.errorKind.value).toBe('unauthorized')
    expect(resource.canAutoRefresh.value).toBe(false)
  })
  it('aborts old reads and ignores their response even if the transport ignores cancellation', async () => {
    const first = deferred<number>(), second = deferred<number>()
    const fetch = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const resource = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' })
    const old = resource.load(), current = resource.load()
    expect(fetch.mock.calls[0][0].signal.aborted).toBe(true)
    second.resolve(2)
    expect(await current).toBe(true)
    first.resolve(1)
    expect(await old).toBe(false)
    expect(resource.data.value).toBe(2)
  })

  it('does not let an old error clear the current loading state', async () => {
    const first = deferred<number>(), second = deferred<number>()
    const fetch = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const resource = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' })
    const old = resource.load(), current = resource.load()
    first.reject(new Error('old failure'))
    await old
    expect(resource.loading.value).toBe(true)
    expect(resource.error.value).toBe('')
    second.resolve(2)
    await current
    expect(resource.loading.value).toBe(false)
  })

  it('retains good data on failure but exposes the failure separately', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(1).mockRejectedValueOnce(new Error('timeout'))
    const resource = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' })
    await resource.load()
    expect(await resource.load()).toBe(false)
    expect(resource.data.value).toBe(1)
    expect(resource.error.value).toBe('failed')
    expect(resource.loaded.value).toBe(true)
  })

  it('resets immediately and prevents an outstanding read from repopulating another selection', async () => {
    const pending = deferred<number>()
    const resource = useRemoteResource({ initial: () => 0, fetch: () => pending.promise, errorMessage: 'failed' })
    const request = resource.load()
    resource.reset()
    pending.resolve(1)
    await request
    expect(resource.data.value).toBe(0)
    expect(resource.loaded.value).toBe(false)
  })

  it('keeps a mutation result ahead of an outstanding read', async () => {
    const pending = deferred<number>()
    const resource = useRemoteResource({ initial: () => 0, fetch: () => pending.promise, errorMessage: 'failed' })
    const request = resource.load()
    resource.replace(2)
    pending.resolve(1)
    await request
    expect(resource.data.value).toBe(2)
    expect(resource.loaded.value).toBe(true)
  })

  it('aborts on disposal and suppresses late responses and subsequent loads', async () => {
    const pending = deferred<number>()
    const fetch = vi.fn(() => pending.promise)
    const scope = effectScope()
    const resource = scope.run(() => useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' }))!
    const request = resource.load()
    scope.stop()
    pending.resolve(1)
    expect(await request).toBe(false)
    expect(await resource.load()).toBe(false)
    expect(resource.data.value).toBe(0)
    expect(fetch).toHaveBeenCalledOnce()
  })
})

it('exposes initial, refreshing and stale state with the last successful read time', async () => {
 const pending = deferred<number>()
 const fetch = vi.fn().mockResolvedValueOnce(4).mockReturnValueOnce(pending.promise)
 const r = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' })
 expect(r.phase.value).toBe('idle')
 await r.load()
 const stamp = r.lastSuccessAt.value
 expect(stamp).toBeTruthy()
 const request = r.load()
 expect(r.phase.value).toBe('refreshing')
 expect(r.refreshing.value).toBe(true)
 pending.reject({ code: 'ERR_NETWORK' })
 await request
 expect(r.phase.value).toBe('stale')
 expect(r.errorKind.value).toBe('network')
 expect(r.lastSuccessAt.value).toBe(stamp)
 expect(r.data.value).toBe(4)
 r.reset()
 expect(r.phase.value).toBe('idle')
 expect(r.lastSuccessAt.value).toBeNull()
})

it.each([401, 403])('clears unauthorized cached data on %s without owning global authentication', async status => {
 const fetch = vi.fn().mockResolvedValueOnce(42).mockRejectedValueOnce({ response: { status, data: { message: 'denied' } } })
 const r = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' })
 await r.load(); await r.load()
 expect(r.phase.value).toBe(status === 401 ? 'unauthorized' : 'forbidden')
 expect(r.loaded.value).toBe(false)
 expect(r.data.value).toBe(0)
 expect(r.lastSuccessAt.value).toBeNull()
})

it('bounds loading even when the transport never settles and retains a known result', async () => {
 vi.useFakeTimers()
 try {
  const fetch = vi.fn().mockResolvedValueOnce(7).mockReturnValueOnce(new Promise(() => {}))
  const r = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed', timeoutMs: 100 })
  await r.load()
  const request = r.load()
  await vi.advanceTimersByTimeAsync(101)
  expect(await request).toBe(false)
  expect(r.phase.value).toBe('stale')
  expect(r.errorKind.value).toBe('timeout')
  expect(r.data.value).toBe(7)
  expect(fetch.mock.calls[1][0].signal.aborted).toBe(true)
 } finally { vi.useRealTimers() }
})

it('bounds automatic failure retries while allowing manual recovery', async () => {
 const fetch = vi.fn().mockRejectedValue(new Error('offline'))
 const r = useRemoteResource({ initial: () => 0, fetch, errorMessage: 'failed' })
 for (let i=0; i<3; i++) await r.load()
 expect(r.canAutoRefresh.value).toBe(false)
 fetch.mockResolvedValue(3)
 expect(await r.load()).toBe(true)
 expect(r.canAutoRefresh.value).toBe(true)
 expect(r.failureCount.value).toBe(0)
})

it.each([408, 504])('identifies a backend read budget response (%s) as a timeout', async status => {
 const r = useRemoteResource({initial:()=>0,fetch:()=>Promise.reject({response:{status}}),errorMessage:'timed out'})
 await r.load()
 expect(r.errorKind.value).toBe('timeout')
 expect(r.phase.value).toBe('error')
})
