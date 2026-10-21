import { api } from './client'

export type ObservationIntentStatus = 'held' | 'pending' | 'claimed' | 'applied'
export interface ObservationIntentItem {
  id: string
  node_id: string
  status: ObservationIntentStatus
  reason: string
  asset_relation: 'unknown' | 'same_asset' | 'different_asset' | 'node_missing' | 'asset_deleted'
  attempts: string
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
