import { api } from './client'

export type ObservationIntentStatus = 'held' | 'pending' | 'claimed' | 'applied' | 'retired'
export interface ObservationIntentItem {
  id: string
  node_id: string
  status: ObservationIntentStatus
  reason: string
  asset_relation: 'unknown' | 'same_asset' | 'different_asset' | 'node_missing' | 'asset_deleted'
  attempts: string
  retained_bytes?: string
  material_retired_at?: string
  coverage_gap?: boolean
  replay_unavailable?: boolean
  material_gap_reason?: string
  material_cleanup_state?: string
  material_bytes: string
  part_count: string
  enqueued_at: string
  available_at: string
  disposition_updated_at: string
  lease_until?: string
  material_verified: false
}
export interface ObservationIntentPage {
  items: ObservationIntentItem[]
  status: ObservationIntentStatus
  has_more: boolean
  next_cursor?: string
}
export async function fetchObservationIntents(status: ObservationIntentStatus, cursor?: string): Promise<ObservationIntentPage> {
  const response = await api.get('/admin/node-observation-intents', { params: { status, limit: 50, ...(cursor ? { cursor } : {}) } })
  const page = response.data?.data as ObservationIntentPage | undefined
  if (!page || page.status !== status || !Array.isArray(page.items) || typeof page.has_more !== 'boolean'
    || (page.has_more && (typeof page.next_cursor !== 'string' || !page.next_cursor))) {
    throw new Error('观察队列响应不完整，请重试。')
  }
  return page
}

export interface ObservationIntentRetryRequest { expected_attempts: string; reason: string }
export interface ObservationIntentRetryReceipt { id: string; expected_attempts: string; audit_id: string; status: 'accepted' }
export async function retryObservationIntent(id: string, request: ObservationIntentRetryRequest): Promise<ObservationIntentRetryReceipt> {
  const response = await api.post(`/admin/node-observation-intents/${id}/retry`, request)
  const receipt = response.data?.data as ObservationIntentRetryReceipt | undefined
  if (!receipt || receipt.id !== id || receipt.expected_attempts !== request.expected_attempts || receipt.status !== 'accepted' || typeof receipt.audit_id !== 'string' || !/^[1-9][0-9]*$/.test(receipt.audit_id)) throw new Error('观察重试结果需要核对。')
  return receipt
}
