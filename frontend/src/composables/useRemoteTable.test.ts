import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { useRemoteTable } from './useRemoteTable'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

describe('useRemoteTable', () => {
  it('distinguishes the first load from a refresh while retaining the current rows', async () => {
    const offset = ref(0), limit = ref(50)
    const second = deferred<{ items: number[]; total: number; aggregates: { active: number }; facets: { status: string[] } }>()
    const fetchPage = vi.fn()
      .mockResolvedValueOnce({ items: [1, 2], total: 2, aggregates: { active: 2 }, facets: { status: ['active'] } })
      .mockReturnValueOnce(second.promise)
    const table = useRemoteTable({ offset, limit, fetchPage, errorMessage: 'failed' })

    const firstLoad = table.load()
    expect(table.initialLoading.value).toBe(true)
    await firstLoad
    expect(table.items.value).toEqual([1, 2])
    expect(table.aggregates.value.active).toBe(2)
    expect(table.facets.value.status).toEqual(['active'])

    const refresh = table.load()
    expect(table.refreshing.value).toBe(true)
    expect(table.items.value).toEqual([1, 2])
    second.resolve({ items: [3], total: 1, aggregates: { active: 1 }, facets: { status: ['active'] } })
    await refresh
    expect(table.items.value).toEqual([3])
    expect(table.aggregates.value.active).toBe(1)
    expect(table.loading.value).toBe(false)
  })

  it('ignores a late response after a newer request completes', async () => {
    const offset = ref(0), limit = ref(50)
    const first = deferred<{ items: number[]; total: number }>()
    const second = deferred<{ items: number[]; total: number }>()
    const fetchPage = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const table = useRemoteTable({ offset, limit, fetchPage, errorMessage: 'failed' })

    const firstLoad = table.load()
    const secondLoad = table.load()
    second.resolve({ items: [2], total: 1 })
    await secondLoad
    first.resolve({ items: [1], total: 1 })
    await firstLoad
    expect(table.items.value).toEqual([2])
  })

  it('aborts the previous request when a newer load starts', async () => {
    const offset = ref(0), limit = ref(50)
    const first = deferred<{ items: number[]; total: number }>()
    const second = deferred<{ items: number[]; total: number }>()
    const signals: AbortSignal[] = []
    const fetchPage = vi.fn(({ signal }: { signal: AbortSignal }) => {
      signals.push(signal)
      return signals.length === 1 ? first.promise : second.promise
    })
    const table = useRemoteTable({ offset, limit, fetchPage, errorMessage: 'failed' })

    const firstLoad = table.load()
    const secondLoad = table.load()
    expect(signals[0].aborted).toBe(true)
    expect(signals[1].aborted).toBe(false)

    second.resolve({ items: [2], total: 1 })
    await secondLoad
    first.resolve({ items: [1], total: 1 })
    await firstLoad
    expect(table.items.value).toEqual([2])
    expect(table.error.value).toBe('')
  })

  it('moves an empty out-of-range page to the last valid page and reloads it', async () => {
    const offset = ref(100), limit = ref(50)
    const onOffsetCorrected = vi.fn()
    const fetchPage = vi.fn()
      .mockResolvedValueOnce({ items: [], total: 75 })
      .mockResolvedValueOnce({ items: [75], total: 75 })
    const table = useRemoteTable({ offset, limit, fetchPage, errorMessage: 'failed', onOffsetCorrected })

    expect(await table.load()).toBe(true)
    expect(offset.value).toBe(50)
    expect(onOffsetCorrected).toHaveBeenCalledOnce()
    expect(fetchPage).toHaveBeenCalledTimes(2)
    expect(table.items.value).toEqual([75])
  })
})
