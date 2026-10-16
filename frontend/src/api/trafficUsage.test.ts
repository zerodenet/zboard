import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchTrafficBreakdown, fetchTrafficNodeSeries, fetchTrafficUsagePage, fetchTrafficUsageStatistics } from './trafficUsage'

afterEach(() => vi.unstubAllGlobals())

describe('traffic usage page projection', () => {
  it('does not normalize malformed pages into successful empty usage history', async () => {
    for (const data of [null, {}, { total: 0 }, { items: [] }, { items: {}, total: 0 }, { items: [], total: -1 }, { items: [], total: '0' }]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data }) }))
      await expect(fetchTrafficUsagePage()).rejects.toThrow('分页响应不完整')
    }
  })
  it('preserves deliberately unknown totals instead of normalizing null to zero', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: { items: [], page: { total: null }, total: null, aggregates: null } }) })
    vi.stubGlobal('fetch', fetch)
    const result = await fetchTrafficUsagePage({ includeTotals: false })
    expect(result.total).toBeNull()
    expect(result.page.total).toBeNull()
    expect(result.aggregates).toBeNull()
    expect(fetch.mock.calls[0]![0]).toContain('include_totals=false')
  })
  it('uses a separately cancellable statistics projection and rejects incomplete statistics', async () => {
    const data = { total: 0, as_of: '2026-09-01T00:00:00Z', bucket: 'hour', aggregates: {
      raw_bytes: 0, used_bytes: 0, user_count: 0, subscription_count: 0, node_count: 0, protocol_endpoint_count: 0,
    } }
    const fetch = vi.fn().mockResolvedValueOnce({ ok: true, json: async () => ({ data }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ data: {} }) })
    vi.stubGlobal('fetch', fetch)
    const signal = new AbortController().signal
    expect(await fetchTrafficUsageStatistics({ subscriptionId: 12 }, true, { signal })).toEqual(data)
    expect(fetch.mock.calls[0]![0]).toContain('/admin/traffic/records?')
    expect(fetch.mock.calls[0]![0]).toContain('view=usage_summary')
    expect(fetch.mock.calls[0]![1].signal).toBe(signal)
    await expect(fetchTrafficUsageStatistics()).rejects.toThrow('响应不完整')
  })
  it('preserves page-scoped reference maps and cancellation through normalization', async () => {
    const facets = { nodes: { 99: { id: 99, kind: 'node', display_name: 'Outside ranking' } },
      subscriptions: { 123: { id: 123, kind: 'subscription', display_name: 'Missing', missing: true } } }
    const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: {
      items: [{ id: 1, node_id: 99, subscription_id: 123 }],
      page: { total: 500, limit: 25, next_cursor: 'next' }, aggregates: { used_bytes: 50 }, facets, bucket: 'hour',
    } }) })
    vi.stubGlobal('fetch', fetch)
    const signal = new AbortController().signal
    const result = await fetchTrafficUsagePage({ subscriptionId: 123, limit: 25 }, false, { signal })
    expect(result.facets).toEqual(facets)
    expect(result.items[0]!.node_id).toBe(99)
    expect(result.total).toBe(500)
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining('/traffic/records?'), expect.objectContaining({ signal }))
  })
  it('keeps absent references empty instead of borrowing unrelated chart labels', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: { items: [], total: 0 } }) }))
    expect((await fetchTrafficUsagePage()).facets).toEqual({})
  })
  it('loads bounded dimension statistics with all filters and cancellation', async () => {
    const counters = { raw_bytes: 10, used_bytes: 20, upload_bytes: 3, download_bytes: 7, record_count: 1 }
    const data = { dimension: 'node', items: [{ id: 0, name: '', missing: true, ...counters }], totals: counters, total: 1, offset: 0, limit: 25, as_of: '2026-10-16T00:00:00Z' }
    const fetch = vi.fn().mockResolvedValueOnce({ ok: true, json: async () => ({ data }) }).mockResolvedValueOnce({ ok: true, json: async () => ({ data: {} }) })
    vi.stubGlobal('fetch', fetch)
    const signal = new AbortController().signal
    expect(await fetchTrafficBreakdown({ dimension: 'node', userId: '12', subscriptionId: 2, protocolEndpointId: 3, from: '2026-10-01', to: '2026-10-16', offset: 0, limit: 25 }, true, { signal })).toEqual(data)
    expect(fetch.mock.calls[0]![0]).toContain('view=breakdown&dimension=node')
    expect(fetch.mock.calls[0]![0]).toContain('user_id=12&subscription_id=2&protocol_endpoint_id=3')
    expect(fetch.mock.calls[0]![1].signal).toBe(signal)
    await expect(fetchTrafficBreakdown({ dimension: 'node' })).rejects.toThrow('响应不完整')
  })
  it('rejects invalid dimension filters before making any request', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await expect(fetchTrafficBreakdown({ dimension: 'node', userId: 'not-an-id' })).rejects.toThrow()
    expect(fetch).not.toHaveBeenCalled()
  })
  it('keeps endpoint drilldown on the node chart', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ data: {} }) })
    vi.stubGlobal('fetch', fetch)
    await fetchTrafficNodeSeries({ protocolEndpointId: 123 }, true)
    expect(fetch.mock.calls[0]![0]).toContain('protocol_endpoint_id=123')
  })
})
