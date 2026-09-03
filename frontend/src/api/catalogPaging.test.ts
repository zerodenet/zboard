import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchAccountSubscriptionsPage, fetchPlanCatalogPage, fetchPlanCatalogSKUs, fetchPlansPage, fetchTrafficSummary } from './client'
import { fetchTrafficTrends } from './readModels'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('axios', () => ({ default: { create: () => ({
  get, interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } },
}) } }))
function params() { return new URL(get.mock.calls.at(-1)![0], 'https://fixture.test').searchParams }
describe('catalog paging API contract', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: { data: { items: [], total: 105, offset: 100, limit: 25 } } })
  })
  it('selects a purchase storefront explicitly without narrowing administrator management reads', async () => {
    await fetchPlanCatalogPage()
    expect(params().get('operation')).toBe('purchase')
    await fetchPlansPage()
    expect(params().has('operation')).toBe(false)
  })
  it('forwards summary cancellation without changing its account/admin scope', async () => {
    const signal = new AbortController().signal
    await fetchTrafficSummary(false, { signal })
    expect(get).toHaveBeenLastCalledWith('/traffic/summary', { signal })
    await fetchTrafficSummary(true, { signal })
    expect(get).toHaveBeenLastCalledWith('/admin/traffic/summary', { signal })
  })
  it('sends operation and plan filters before page selection with request cancellation', async () => {
    const controller = new AbortController()
    await fetchPlanCatalogPage({ operation: 'change', excludePlanId: 12, offset: 9, limit: 9 }, { signal: controller.signal })
    expect(Object.fromEntries(params())).toEqual({ paged: 'true', operation: 'change', exclude_plan_id: '12', offset: '9', limit: '9' })
    expect(get.mock.calls.at(-1)![1].signal).toBe(controller.signal)
    await fetchPlanCatalogPage({ operation: 'renew', planId: 12 })
    expect(params().get('plan_id')).toBe('12')
  })
  it('uses the server anchor-page offset rather than the requested first-page offset', async () => {
    const result = await fetchPlanCatalogSKUs(1, { operation: 'renew', anchorId: 105, offset: 0, limit: 25 })
    expect(params().get('anchor_id')).toBe('105')
    expect(params().get('operation')).toBe('renew')
    expect(result.page.offset).toBe(100)
    expect(result.total).toBe(105)
  })
  it('keeps exact target lookup and management scope in the self-service request', async () => {
    await fetchAccountSubscriptionsPage({ subscriptionId: 201, eligibleFor: 'renew', offset: 0, limit: 1 })
    expect(Object.fromEntries(params())).toEqual({ paged: 'true', subscription_id: '201', eligible_for: 'renew', offset: '0', limit: '1' })
    expect(get.mock.calls.at(-1)![0]).toMatch(/^\/subscriptions\?/)
  })
  it('sends subscription search before pagination', async () => {
    await fetchAccountSubscriptionsPage({ q: 'Historical plan', offset: 100, limit: 25 })
    expect(params().get('q')).toBe('Historical plan')
    expect(params().get('offset')).toBe('100')
  })
  it('does not request an unbounded subscription directory with chart data', async () => {
    const signal = new AbortController().signal
    await fetchTrafficTrends({ from: '2026-09-01', to: '2026-09-02', signal })
    expect(params().get('include_subscriptions')).toBe('false')
    expect(get.mock.calls.at(-1)![1].signal).toBe(signal)
  })
  it('rejects broken page envelopes rather than inventing an empty catalog or subscription list', async () => {
    for (const data of [null, {}, [], { items: [] }, { items: null, total: 0 }, { items: [], total: null }, { items: [], page: {} }]) {
      get.mockResolvedValue({ data: { data } })
      await expect(fetchPlanCatalogPage()).rejects.toThrow('分页响应不完整')
      await expect(fetchPlanCatalogSKUs(1)).rejects.toThrow('分页响应不完整')
      await expect(fetchAccountSubscriptionsPage()).rejects.toThrow('分页响应不完整')
    }
  })
})
