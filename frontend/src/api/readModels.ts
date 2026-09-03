import axios from 'axios'
import { normalizeApiErrorPayload } from '../utils/apiError'
import { API_BASE, getAuthToken } from './client'
import { appendQueryID, type QueryID } from './queryID'

export type EntityKind = 'user' | 'subscription' | 'node' | 'protocol_endpoint' | 'plan' | 'plan_sku' | 'order' | string

export interface EntityReference {
  id: number
  kind: EntityKind
  display_name: string
  secondary?: string
  status?: string
  missing?: boolean
}

export interface EntityReferenceResponse {
  users: Record<string, EntityReference>
  subscriptions: Record<string, EntityReference>
  nodes: Record<string, EntityReference>
  protocol_endpoints: Record<string, EntityReference>
  plans: Record<string, EntityReference>
  plan_skus: Record<string, EntityReference>
  orders: Record<string, EntityReference>
  targets: Record<string, EntityReference>
}

export interface TrafficTrendPoint {
  date: string
  label: string
  upload_bytes: number
  download_bytes: number
  used_bytes: number
  peak_connections: number | null
  record_count: number
}

export interface TrafficTrendResult {
  from: string
  to: string
  points: TrafficTrendPoint[]
  record_count: number
  connection_sample_count: number
  peak_connections: number | null
  truncated: boolean
  subscriptions: EntityReference[]
  as_of: string
}

export interface TrafficTrendQuery {
  admin?: boolean
  userId?: QueryID
  subscriptionId?: QueryID
  nodeId?: QueryID
  protocolEndpointId?: QueryID
  from: string
  to: string
  signal?: AbortSignal
}

export interface AdminEntityReferenceQuery {
  userIds?: number[]
  subscriptionIds?: number[]
  nodeIds?: number[]
  protocolEndpointIds?: number[]
  planIds?: number[]
  planSkuIds?: number[]
  orderIds?: number[]
  targets?: string[]
  signal?: AbortSignal
}

const readModelApi = axios.create({
  baseURL: API_BASE,
  timeout: 8000,
})

readModelApi.interceptors.request.use(config => {
  const token = getAuthToken()
  if (token) {
    config.headers = config.headers || {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

readModelApi.interceptors.response.use(
  response => response,
  cause => {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    return Promise.reject(cause)
  },
)

function unwrap<T>(response: any): T {
  return response.data?.data as T
}

function positiveUnique(values: number[] | undefined): number[] {
  return Array.from(new Set((values || []).filter(value => Number.isInteger(value) && value > 0)))
}

function appendIDs(query: URLSearchParams, key: string, values: number[] | undefined) {
  const normalized = positiveUnique(values)
  if (normalized.length) query.set(key, normalized.join(','))
}

export function emptyEntityReferenceResponse(): EntityReferenceResponse {
  return {
    users: {},
    subscriptions: {},
    nodes: {},
    protocol_endpoints: {},
    plans: {},
    plan_skus: {},
    orders: {},
    targets: {},
  }
}

export function fallbackEntityReference(kind: EntityKind, id: number): EntityReference {
  const labels: Record<string, string> = {
    user: '用户',
    subscription: '订阅',
    node: '节点',
    protocol_endpoint: '协议端点',
    plan: '套餐',
    plan_sku: '套餐规格',
    order: '订单',
  }
  return {
    id,
    kind,
    display_name: labels[kind] || '关联对象',
  }
}

export async function fetchTrafficTrends(query: TrafficTrendQuery): Promise<TrafficTrendResult> {
  const params = new URLSearchParams({ from: query.from, to: query.to })
  // Option browsing is a separate bounded subscription read, not chart data.
  params.set('include_subscriptions', 'false')
  appendQueryID(params, 'user_id', query.userId)
  appendQueryID(params, 'subscription_id', query.subscriptionId)
  appendQueryID(params, 'node_id', query.nodeId)
  appendQueryID(params, 'protocol_endpoint_id', query.protocolEndpointId)
  const path = query.admin ? '/admin/traffic/trends' : '/traffic/trends'
  return unwrap<TrafficTrendResult>(await readModelApi.get(`${path}?${params}`, { signal: query.signal }))
}

export async function fetchAdminEntityReferences(query: AdminEntityReferenceQuery): Promise<EntityReferenceResponse> {
  const params = new URLSearchParams()
  appendIDs(params, 'user_ids', query.userIds)
  appendIDs(params, 'subscription_ids', query.subscriptionIds)
  appendIDs(params, 'node_ids', query.nodeIds)
  appendIDs(params, 'protocol_endpoint_ids', query.protocolEndpointIds)
  appendIDs(params, 'plan_ids', query.planIds)
  appendIDs(params, 'plan_sku_ids', query.planSkuIds)
  appendIDs(params, 'order_ids', query.orderIds)
  for (const target of Array.from(new Set((query.targets || []).map(value => value.trim()).filter(Boolean)))) {
    params.append('targets', target)
  }
  const suffix = params.toString()
  return unwrap<EntityReferenceResponse>(await readModelApi.get(`/admin/entity-references${suffix ? `?${suffix}` : ''}`, { signal: query.signal }))
}
