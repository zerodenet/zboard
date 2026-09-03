import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchTrafficUsagePage, fetchTrafficUsageStatistics } from './trafficUsage'

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
})
