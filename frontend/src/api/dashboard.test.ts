import { describe, expect, it, vi } from 'vitest'
import { api } from './client'
import { fetchDashboardCurrent, fetchDashboardStatistics, fetchDashboardTrend } from './dashboard'
vi.mock('./client', () => ({ api: { get: vi.fn() } }))
describe('focused dashboard reads', () => {
 it('uses explicit views and propagates cancellation without changing period scope', async () => {
  vi.mocked(api.get).mockResolvedValue({data:{data:{as_of:'now'}}})
  const signal = new AbortController().signal
  await fetchDashboardCurrent({signal})
  expect(api.get).toHaveBeenLastCalledWith('/admin/dashboard/overview',{params:{range:'today',view:'current'},signal})
  await fetchDashboardStatistics('month',{signal})
  expect(api.get).toHaveBeenLastCalledWith('/admin/dashboard/overview',{params:{range:'month',view:'statistics'},signal})
  await fetchDashboardTrend('7d',{signal})
  expect(api.get).toHaveBeenLastCalledWith('/admin/dashboard/overview',{params:{range:'7d',view:'trend'},signal})
 })
})
