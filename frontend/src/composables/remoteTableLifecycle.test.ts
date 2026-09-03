import { effectScope, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useCursorTable } from './useCursorTable'
import { useRemoteTable } from './useRemoteTable'
import { keyedLoad } from './keyedLoad'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function page(id: number) {
  return { items: [id], total: 1, page: { next_cursor: 'next', previous_cursor: null }, aggregates: { used: id }, facets: { names: [id] } }
}
describe.each(['offset', 'cursor'])('remote table lifecycle (%s)', kind => {
  function table(fetchPage: () => Promise<ReturnType<typeof page>>) {
    return kind === 'offset'
      ? useRemoteTable({ offset: ref(0), limit: ref(25), fetchPage, errorMessage: 'failed' })
      : useCursorTable({ fetchPage, errorMessage: 'failed' })
  }
  it('clears rows, aggregates and facets together and ignores a late old-filter read', async () => {
    const late = deferred<ReturnType<typeof page>>()
    const fetchPage = vi.fn().mockResolvedValueOnce(page(1)).mockReturnValueOnce(late.promise)
    const state = table(fetchPage)
    await state.load()
    const pending = state.load()
    state.reset()
    late.resolve(page(2))
    expect(await pending).toBe(false)
    expect(state.items.value).toEqual([])
    expect(state.aggregates.value).toEqual({})
    expect(state.facets.value).toEqual({})
    expect(state.hasLoaded.value).toBe(false)
  })
  it('cannot restart a request after scope disposal', async () => {
    const scope = effectScope()
    const late = deferred<ReturnType<typeof page>>()
    const fetchPage = vi.fn().mockReturnValue(late.promise)
    const state = scope.run(() => table(fetchPage))!
    const pending = state.load()
    scope.stop()
    late.resolve(page(1))
    expect(await pending).toBe(false)
    expect(await state.load()).toBe(false)
    expect(fetchPage).toHaveBeenCalledOnce()
  })
})
it('does not restart an offset correction after its query was superseded', async () => {
  const hook = deferred<void>()
  const offset = ref(50)
  const fetchPage = vi.fn().mockResolvedValueOnce(page(1)).mockResolvedValueOnce(page(2))
  const state = useRemoteTable({ offset, limit: ref(25), fetchPage, errorMessage: 'failed', onOffsetCorrected: () => hook.promise })
  const old = state.load()
  await Promise.resolve()
  expect(offset.value).toBe(0)
  await state.load()
  hook.resolve()
  expect(await old).toBe(false)
  expect(fetchPage).toHaveBeenCalledTimes(2)
  expect(state.items.value).toEqual([2])
})
it('keyed loads skip unchanged queries, reset changed scopes and permit explicit retry', async () => {
  let key = 'A'
  const resource = { load: vi.fn().mockResolvedValue(true), reset: vi.fn() }
  const load = keyedLoad(() => key, resource)
  await load(); await load()
  expect(resource.load).toHaveBeenCalledOnce()
  key = 'B'
  await load(); await load(true)
  expect(resource.load).toHaveBeenCalledTimes(3)
  expect(resource.reset).toHaveBeenCalledTimes(2)
})
