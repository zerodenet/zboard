import { API_BASE, getAuthToken, normalizePageResult, type PageResult } from './client'
import { normalizeApiErrorPayload } from '../utils/apiError'

export type TrafficUsageBucket = 'minute' | 'hour' | 'day'

export interface TrafficUsageRecord {
  id: number
  user_id: number
  subscription_id?: number
  node_id: number
  raw_bytes: number
  upload_bytes: number
  download_bytes: number
  protocol_multiplier_milli: number
  used_bytes: number
  record_at: string
  record_count: number
}

export interface TrafficUsageAggregates extends Record<string, unknown> {
  raw_bytes: number
  used_bytes: number
  user_count: number
  subscription_count: number
  node_count: number
  protocol_endpoint_count: number
}

export interface TrafficUsagePage extends PageResult<TrafficUsageRecord, TrafficUsageAggregates> {
  bucket: TrafficUsageBucket
}

export interface TrafficNodeReference {
  id: number
  kind: 'node'
  display_name: string
  secondary?: string
  status?: string
  missing?: boolean
}

export interface TrafficNodeSeriesPoint {
  record_at: string
  node_id: number
  raw_bytes: number
  upload_bytes: number
  download_bytes: number
  used_bytes: number
  record_count: number
}

export interface TrafficNodeSeries {
  bucket: TrafficUsageBucket
  from: string
  to: string
  points: TrafficNodeSeriesPoint[]
  nodes: TrafficNodeReference[]
  truncated: boolean
  node_limit: number
  as_of: string
}

export interface TrafficUsageQuery {
  bucket?: TrafficUsageBucket
  userId?: number
  nodeId?: number
  subscriptionId?: number
  cursor?: string
  from?: string
  to?: string
  limit?: number
}

async function requestData<T>(path: string, signal?: AbortSignal): Promise<T> {
  const token = getAuthToken()
  const response = await fetch(`${API_BASE}${path}`, {
    signal,
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    const normalized = normalizeApiErrorPayload(payload)
    throw new Error(normalized?.message || `流量数据请求失败（HTTP ${response.status}）。`)
  }
  if (!payload || typeof payload !== 'object' || !('data' in payload)) {
    throw new Error('流量数据响应缺少 data。')
  }
  return payload.data as T
}

function appendUsageQuery(query: URLSearchParams, params: TrafficUsageQuery) {
  query.set('paged', 'true')
  query.set('bucket', params.bucket || 'hour')
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.nodeId) query.set('node_id', String(params.nodeId))
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  if (params.cursor) query.set('cursor', params.cursor)
  if (params.from) query.set('from', params.from)
  if (params.to) query.set('to', params.to)
  if (params.limit !== undefined) query.set('limit', String(params.limit))
}

function normalizeTrafficUsageBucket(value: unknown): TrafficUsageBucket {
  if (value === 'minute' || value === 'day') return value
  return 'hour'
}

export async function fetchTrafficUsagePage(
  params: TrafficUsageQuery = {},
  admin = false,
  options: { signal?: AbortSignal } = {},
): Promise<TrafficUsagePage> {
  const query = new URLSearchParams()
  appendUsageQuery(query, params)
  const data = await requestData<any>(`${admin ? '/admin' : ''}/traffic/records?${query}`, options.signal)
  return {
    ...normalizePageResult<TrafficUsageRecord, TrafficUsageAggregates>(data, 0, params.limit || (admin ? 50 : 25)),
    bucket: normalizeTrafficUsageBucket(data?.bucket),
  }
}

export async function fetchTrafficNodeSeries(
  params: Pick<TrafficUsageQuery, 'bucket' | 'userId' | 'nodeId' | 'subscriptionId' | 'from' | 'to'> = {},
  admin = false,
  options: { signal?: AbortSignal } = {},
): Promise<TrafficNodeSeries> {
  const query = new URLSearchParams()
  query.set('view', 'node_series')
  query.set('bucket', params.bucket || 'hour')
  if (params.userId) query.set('user_id', String(params.userId))
  if (params.nodeId) query.set('node_id', String(params.nodeId))
  if (params.subscriptionId) query.set('subscription_id', String(params.subscriptionId))
  if (params.from) query.set('from', params.from)
  if (params.to) query.set('to', params.to)
  return requestData<TrafficNodeSeries>(`${admin ? '/admin' : ''}/traffic/records?${query}`, options.signal)
}
