import { api, type ApiRequestOptions } from './client'

export type DashboardRange = 'today' | '7d' | '30d' | 'month'

export interface DashboardPeriod {
  range: DashboardRange
  from: string
  to: string
  previous_from: string
  previous_to: string
  bucket: 'hour' | 'day'
  timezone: string
}

export interface DashboardBusinessOverview {
  revenue_cents: number
  previous_revenue_cents: number
  paid_orders: number
  previous_paid_orders: number
  new_users?: number
  previous_new_users?: number
  previous_renew_orders?: number
  new_orders: number
  renew_orders: number
  new_subscriptions: number
  previous_new_subscriptions: number
  active_subscriptions: number
  expiring_within_3d: number
  currency?: string
  mixed_currency: boolean
}

export interface DashboardServiceOverview {
  active_subscriptions: number | null
  active_flows: number | null
  previous_traffic_bytes?: number
  traffic_bytes: number
  online_nodes: number
  enabled_nodes: number
}

export interface DashboardSubscriptionHealth {
  expiring_within_24h: number
  expiring_within_3d: number
  expiring_within_7d: number
  quota_exhausted: number
}

export interface DashboardAttentionOverview {
  nodes_offline: number
  deployments_unresolved: number
  tickets_pending: number
}

export interface DashboardInfrastructureOverview {
  nodes_total: number
  nodes_enabled: number
  connector_online: number
  ssh_verified: number
  traffic_ready: number
  protocol_endpoints: number
  active_protocol_endpoints: number
  published_plans: number
  unresolved_deployments: number
}

export interface DashboardTrendPoint {
  bucket_start: string
  revenue_cents: number
  paid_orders: number
  new_orders: number
  renew_orders: number
}

export interface DashboardOverview {
  period: DashboardPeriod
  business: DashboardBusinessOverview
  service: DashboardServiceOverview
  subscriptions: DashboardSubscriptionHealth
  attention: DashboardAttentionOverview
  infrastructure: DashboardInfrastructureOverview
  coverage: {
    principal_flows: boolean
  }
  trend: DashboardTrendPoint[]
  as_of: string
}

export interface DashboardTrafficRanking {
  id: number
  name: string
  traffic_bytes: number
  previous_traffic_bytes: number
}

export interface DashboardTrafficRankings {
  period: DashboardPeriod
  nodes: DashboardTrafficRanking[]
  users: DashboardTrafficRanking[]
  as_of: string
}

export async function fetchDashboardOverview(range: DashboardRange, options: ApiRequestOptions = {}): Promise<DashboardOverview> {
  const response = await api.get('/admin/dashboard/overview', { params: { range }, signal: options.signal })
  return response.data?.data
}

export async function fetchDashboardTrafficRankings(range: DashboardRange, options: ApiRequestOptions & { dimension?: 'nodes' | 'users' } = {}): Promise<DashboardTrafficRankings> {
  const response = await api.get('/admin/dashboard/traffic-rankings', { params: { range, dimension: options.dimension }, signal: options.signal })
  return response.data?.data
}
