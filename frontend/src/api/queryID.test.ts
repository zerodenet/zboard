import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchTrafficReconciliationPage } from './client'
import { fetchTrafficTrends } from './readModels'
import { fetchTrafficNodeSeries, fetchTrafficUsagePage, fetchTrafficUsageStatistics } from './trafficUsage'
import { optionalQueryID } from './queryID'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('axios', () => ({ default: { create: () => ({
  get, interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } },
}) } }))
describe('traffic query scope validation', () => {
  beforeEach(() => { get.mockReset(); vi.stubGlobal('fetch', vi.fn()) })
  afterEach(() => vi.unstubAllGlobals())
  it.each(['bad', '0', '-1', '1.5', '1e2', '0x10', '9007199254740993', ' ', NaN, Infinity, 0])('rejects %s instead of dropping or rounding the filter', async value => {
    for (const field of ['userId', 'subscriptionId', 'nodeId']) {
      await expect(fetchTrafficUsagePage({ [field]: value })).rejects.toThrow('正整数编号')
      await expect(fetchTrafficUsageStatistics({ [field]: value })).rejects.toThrow('正整数编号')
      await expect(fetchTrafficNodeSeries({ [field]: value })).rejects.toThrow('正整数编号')
      await expect(fetchTrafficTrends({ from: '2026-09-01', to: '2026-09-02', [field]: value })).rejects.toThrow('正整数编号')
    }
    await expect(fetchTrafficReconciliationPage({ userId: value })).rejects.toThrow('正整数编号')
    await expect(fetchTrafficReconciliationPage({ subscriptionId: value })).rejects.toThrow('正整数编号')
    expect(fetch).not.toHaveBeenCalled()
    expect(get).not.toHaveBeenCalled()
  })
  it('accepts numeric or decimal-string IDs and omits only absent filters', async () => {
    expect(optionalQueryID(undefined, 'ID')).toBeUndefined()
    expect(optionalQueryID('', 'ID')).toBeUndefined()
    expect(optionalQueryID(' 00130 ', 'ID')).toBe(130)
    get.mockResolvedValue({ data: { data: { items: [], total: 0 } } })
    await fetchTrafficReconciliationPage({ userId: '130', subscriptionId: 12 })
    const query = new URL(get.mock.calls[0]![0], 'https://fixture.invalid').searchParams
    expect(query.get('user_id')).toBe('130')
    expect(query.get('subscription_id')).toBe('12')
  })
})
