import { describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('./client', () => ({ api: { get: mocks.get } }))
import { fetchObservationIntents } from './nodeObservationIntents'
describe('observation header API', () => {
  it('requests only the global single-status keyset page', async () => {
    mocks.get.mockResolvedValueOnce({ data: { data: { status: 'held', items: [], has_more: false } } })
    expect(await fetchObservationIntents('held')).toEqual({ status: 'held', items: [], has_more: false })
    expect(mocks.get).toHaveBeenLastCalledWith('/admin/node-observation-intents', { params: { status: 'held', limit: 50 } })
    mocks.get.mockResolvedValueOnce({ data: { data: { status: 'pending', items: [], has_more: false } } })
    await fetchObservationIntents('pending', 'opaque-cursor')
    expect(mocks.get).toHaveBeenLastCalledWith('/admin/node-observation-intents', { params: { status: 'pending', limit: 50, cursor: 'opaque-cursor' } })
  })
  it.each(['9007199254740993', '18446744073709551615'])('keeps exact wire diagnostic decimal %s', async value => {
    const item = { id: 'd'.repeat(64), node_id: '18446744073709551615', status: 'held', reason: 'apply_byte_budget', asset_relation: 'unknown', attempts: value, material_bytes: value, part_count: value, enqueued_at: '2026-10-21T01:00:00Z', available_at: '2026-10-21T01:00:00Z', disposition_updated_at: '2026-10-21T01:00:00Z', material_verified: false }
    mocks.get.mockResolvedValueOnce({ data: { data: { status: 'held', items: [item], has_more: false } } })
    const result = await fetchObservationIntents('held')
    expect(result.items[0].attempts).toBe(value)
    expect(result.items[0].material_bytes).toBe(value)
    expect(result.items[0].part_count).toBe(value)
    expect(result.items[0].material_verified).toBe(false)
  })
  it('rejects wrong-status and incomplete responses rather than clearing known data', async () => {
    for (const data of [null, {}, { status: 'pending', items: [], has_more: false }, { status: 'held', items: [], has_more: true }]) {
      mocks.get.mockResolvedValueOnce({ data: { data } })
      await expect(fetchObservationIntents('held')).rejects.toThrow('响应不完整')
    }
  })
})
