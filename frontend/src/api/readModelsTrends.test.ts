import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchTrafficTrends } from './readModels'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
// Response-contract unit tests use the shared API boundary; transport/session
// behavior is exercised with real Axios adapters in protectedClientsSession.test.ts.
vi.mock('./client', () => ({ authenticatedAPI: { get } }))

const query = { admin: true, userId: 2, subscriptionId: 3, nodeId: 4, protocolEndpointId: 5, from: '2026-09-01', to: '2026-09-02' }
const scope = { administrative: true, user_id: 2, subscription_id: 3, node_id: 4, protocol_endpoint_id: 5, time_zone: 'UTC' }
function pending() {
  return { projection_pending: true, scope: { ...scope }, from: query.from, to: query.to,
    points: null, record_count: null, connection_sample_count: null, peak_connections: null,
    truncated: null, subscriptions: null, as_of: null }
}
function complete() {
  return { projection_pending: false, scope: { ...scope }, from: query.from, to: query.to,
    points: [], record_count: 0, connection_sample_count: 0, peak_connections: null,
    truncated: false, subscriptions: [], as_of: '2026-09-01T00:00:00Z' }
}
function respond(data: unknown) { get.mockResolvedValue({ data: { data } }) }

describe('traffic trend pending response protocol', () => {
  beforeEach(() => { get.mockReset() })
  it('preserves accepted pending nulls instead of manufacturing an empty snapshot', async () => {
    const value = pending(); respond(value)
    expect(await fetchTrafficTrends(query)).toBe(value)
    expect(value.points).toBeNull()
    expect(value.as_of).toBeNull()
  })
  it('retains a complete zero snapshot as complete with its actual as_of', async () => {
    const value = complete(); respond(value)
    expect(await fetchTrafficTrends(query)).toBe(value)
  })
  it.each(['points', 'record_count', 'connection_sample_count', 'peak_connections', 'truncated', 'subscriptions', 'as_of'])('rejects pending %s masquerading as a measured value', async field => {
    respond({ ...pending(), [field]: field === 'points' || field === 'subscriptions' ? [] : 0 })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('更新状态不完整')
  })
  it.each(['administrative', 'user_id', 'subscription_id', 'node_id', 'protocol_endpoint_id'])('rejects mismatched response %s without broadening its scope', async field => {
    respond({ ...pending(), scope: { ...scope, [field]: field === 'administrative' ? false : 999 } })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('响应范围不一致')
  })
  it('requires scope/calendar metadata for the new complete protocol', async () => {
    respond({ ...complete(), scope: undefined })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('响应范围不一致')
    respond({ ...complete(), scope: { ...scope, time_zone: '' } })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('响应范围不一致')
  })
  it.each([true, false])('rejects a new-protocol response for another date range (pending=%s)', async projection_pending => {
    respond({ ...(projection_pending ? pending() : complete()), from: '2026-08-01' })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('响应范围不一致')
    respond({ ...(projection_pending ? pending() : complete()), to: '2026-09-03' })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('响应范围不一致')
  })
  it('rejects a claimed complete snapshot with null statistics', async () => {
    respond({ ...pending(), projection_pending: false })
    await expect(fetchTrafficTrends(query)).rejects.toThrow('完整快照不完整')
  })
  it('keeps a real server 500 rejected rather than converting it to pending', async () => {
    const failure = { response: { status: 500, data: { message: 'database failed' } } }
    get.mockRejectedValue(failure)
    await expect(fetchTrafficTrends(query)).rejects.toBe(failure)
  })
  it('keeps legacy complete responses readable without inventing protocol metadata', async () => {
    const { projection_pending: _pending, scope: _scope, ...legacy } = complete()
    respond(legacy)
    expect(await fetchTrafficTrends(query)).toBe(legacy)
    expect(legacy).not.toHaveProperty('projection_pending')
  })
  it.each([null, {}, { points: [] }, { ...complete(), projection_pending: undefined, as_of: null }, { ...complete(), projection_pending: undefined, points: null }, { ...complete(), projection_pending: undefined, record_count: null }])('rejects malformed legacy 200 statistics instead of presenting an empty complete snapshot', async value => {
    respond(value)
    await expect(fetchTrafficTrends(query)).rejects.toThrow('完整快照不完整')
  })
})
