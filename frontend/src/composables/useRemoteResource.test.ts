import { effectScope } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useRemoteResource } from './useRemoteResource'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

describe('useRemoteResource', () => {
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
