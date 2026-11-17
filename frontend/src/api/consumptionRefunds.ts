import { authenticatedAPI as api } from './client'

export interface ConsumptionRefundRequest {
  amount_minor: number
  ticket_id: number
  reason: string
  idempotency_key: string
}
export interface ConsumptionRefund {
  id: number
  order_id: number
  user_id: number
  currency: string
  amount_minor: string
  ticket_id: number
  actor_id: number
  reason: string
  entry_id: number
  created_at: string
}
export async function approveConsumptionRefund(orderID: number, request: ConsumptionRefundRequest): Promise<ConsumptionRefund> {
  const { data } = await api.post(`/admin/orders/${orderID}/consumption-refunds`, request)
  return data.data
}
export async function fetchConsumptionRefund(id: number, signal?: AbortSignal): Promise<ConsumptionRefund> {
  const { data } = await api.get(`/admin/consumption-refunds/${id}`, { signal })
  return data.data
}
