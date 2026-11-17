import { beforeEach, expect, it, vi } from 'vitest'
const get = vi.hoisted(() => vi.fn())
vi.mock('./client', () => ({ authenticatedAPI: { get } }))
import { fetchRuntimeJobs } from './runtimeJobs'
beforeEach(() => { get.mockReset(); get.mockResolvedValue({ data: { data: { as_of: 'now' } } }) })
it('requests only the selected runtime partition with cancellation', async () => {
 const signal = new AbortController().signal
 for (const view of ['execution', 'queues', 'runtime', 'plugins'] as const) {
  await fetchRuntimeJobs(signal, view)
  expect(get).toHaveBeenLastCalledWith('/admin/runtime-jobs', { signal, params: { view } })
 }
})
it('retains the full snapshot request for existing callers', async () => {
 await fetchRuntimeJobs()
 expect(get).toHaveBeenCalledWith('/admin/runtime-jobs', { signal: undefined, params: undefined })
})
