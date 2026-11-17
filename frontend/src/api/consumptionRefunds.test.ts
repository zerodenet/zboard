import { afterEach, expect, it, vi } from 'vitest'
import { api } from './client'
import { approveConsumptionRefund, fetchConsumptionRefund } from './consumptionRefunds'

afterEach(() => vi.restoreAllMocks())
it('uses only the administrator refund endpoints and preserves exact server money strings', async () => {
  const fact = { id: 3, order_id: 7, user_id: 2, currency: 'CNY', amount_minor: '9007199254740991', actor_id: 1, ticket_id: 8, entry_id: 9, reason: '工单核准退款', created_at: '2026-10-20T00:00:00Z' }
  const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { data: fact } })
  const get = vi.spyOn(api, 'get').mockResolvedValue({ data: { data: fact } })
  const request = { amount_minor: 9007199254740991, ticket_id: 8, reason: '工单核准退款', idempotency_key: 'e'.repeat(32) }
  expect(await approveConsumptionRefund(7, request)).toEqual(fact)
  expect(post).toHaveBeenCalledWith('/admin/orders/7/consumption-refunds', request)
  const signal = new AbortController().signal
  expect((await fetchConsumptionRefund(3, signal)).amount_minor).toBe('9007199254740991')
  expect(get).toHaveBeenCalledWith('/admin/consumption-refunds/3', { signal })
})
