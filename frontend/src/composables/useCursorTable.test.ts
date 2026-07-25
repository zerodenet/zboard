import { describe, expect, it, vi } from 'vitest'
import { useCursorTable } from './useCursorTable'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

function page(items: number[], next: string | null, previous: string | null, usedBytes = 0) {
  return { items, total: 120, aggregates: { used_bytes: usedBytes }, facets: { status: ['active'] }, page: { next_cursor: next, previous_cursor: previous } }
}

describe('useCursorTable', () => {
  it('retains rows while refreshing and exposes both server cursors', async () => {
    const second = deferred<ReturnType<typeof page>>()
    const fetchPage = vi.fn().mockResolvedValueOnce(page([1, 2], 'older-1', null, 1024)).mockReturnValueOnce(second.promise)
    const table = useCursorTable({ fetchPage, errorMessage: 'failed' })

    await table.load()
    expect(table.nextCursor.value).toBe('older-1')
    expect(table.previousCursor.value).toBeNull()
    expect(table.aggregates.value.used_bytes).toBe(1024)
    expect(table.facets.value.status).toEqual(['active'])
    const refresh = table.load()
    expect(table.refreshing.value).toBe(true)
    expect(table.items.value).toEqual([1, 2])
    second.resolve(page([3], 'older-2', 'newer-2', 2048))
    await refresh
    expect(table.items.value).toEqual([3])
    expect(table.aggregates.value.used_bytes).toBe(2048)
    expect(table.previousCursor.value).toBe('newer-2')
  })

  it('aborts and ignores an older in-flight request', async () => {
    const first = deferred<ReturnType<typeof page>>()
    const second = deferred<ReturnType<typeof page>>()
    const signals: AbortSignal[] = []
    const fetchPage = vi.fn(({ signal }: { signal: AbortSignal }) => {
      signals.push(signal)
      return signals.length === 1 ? first.promise : second.promise
    })
    const table = useCursorTable({ fetchPage, errorMessage: 'failed' })
    const firstLoad = table.load()
    const secondLoad = table.load()
    expect(signals[0].aborted).toBe(true)
    second.resolve(page([2], null, 'newer'))
    await secondLoad
    first.resolve(page([1], 'older', null))
    await firstLoad
    expect(table.items.value).toEqual([2])
  })
})
