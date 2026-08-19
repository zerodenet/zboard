import { API_BASE, getAuthToken } from './client'

export type FairUseObservationRange = '1d' | '3d' | '7d' | '15d'

export interface FairUseCoverageNode {
  node_id: number
  state: 'complete' | 'incomplete' | 'unknown' | string
  reason: string
  core_instance_id?: string
  last_sequence?: number
  continuous_since_at?: string
  last_received_at?: string
  last_gap_from_sequence?: number
  last_gap_to_sequence?: number
  last_gap_at?: string
}

export interface FairUseCoverageSummary {
  state: 'complete' | 'incomplete' | 'unknown' | string
  reason: string
  window_seconds: number
  required_nodes: number
  complete_nodes: number
  nodes: FairUseCoverageNode[]
}

export interface FairUseMetrics {
  subscription_id: number
  user_id: number
  sampled_at: string
  current_active_flows?: number | null
  connection_starts: { window_seconds: number; count: number }
  received_connection_starts: { window_seconds: number; count: number }
  working_nodes: { window_seconds: number; count: number }
  received_working_nodes: { window_seconds: number; count: number }
  last_activity_at?: string | null
  last_received_at?: string | null
  telemetry_completeness: string
  evaluation_ready: boolean
  enforcement_ready: boolean
  event_time_basis: string
  coverage: FairUseCoverageSummary
}

export interface FairUseObservationBucket {
  start_at: string
  end_at: string
  connection_starts: number
  working_nodes: number
}

export interface FairUseObservationSeries {
  subscription_id: number
  range: FairUseObservationRange
  since: string
  until: string
  bucket_seconds: number
  retention_days: number
  time_basis: string
  telemetry_completeness: string
  coverage: FairUseCoverageSummary
  total_connection_starts: number
  distinct_working_nodes: number
  active_buckets: number
  max_connection_starts_per_bucket: number
  p50_connection_starts_per_bucket: number
  p95_connection_starts_per_bucket: number
  max_working_nodes_per_bucket: number
  p50_working_nodes_per_bucket: number
  p95_working_nodes_per_bucket: number
  buckets: FairUseObservationBucket[]
}

export interface FairUseState {
  subscription_id: number
  score: number
  state: 'normal' | 'suspected' | 'violated' | string
  current_active_flows?: number | null
  connection_starts: number
  working_nodes: number
  telemetry_completeness: string
  last_evaluated_at?: string | null
  last_complete_at?: string | null
}

export interface FairUsePolicy {
  scope_type: 'platform' | 'plan' | 'subscription' | string
  scope_id: number
  enabled: boolean
  evaluation_interval_seconds: number
  connection_start_threshold: number
  connection_start_window_seconds: number
  connection_start_penalty: number
  working_node_threshold: number
  working_node_window_seconds: number
  working_node_penalty: number
  score_max: number
  recovery_per_interval: number
  warning_score: number
  violation_score: number
  enforcement_mode: string
  revision: number
}

export interface FairUsePolicyResolution {
  configured: boolean
  override?: FairUsePolicy
  effective: FairUsePolicy
  source: { scope_type: string; scope_id: number }
}

export interface FairUseEvent {
  id: number
  subscription_id: number
  event_type: string
  score_before: number
  score_after: number
  state_before: string
  state_after: string
  metrics: Record<string, unknown>
  reason: string
  occurred_at: string
}

async function getFairUse<T>(path: string, signal?: AbortSignal): Promise<T> {
  const headers = new Headers({ Accept: 'application/json' })
  const token = getAuthToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(`${API_BASE}${path}`, { headers, signal })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(payload?.message || `HTTP ${response.status}`) as Error & { status?: number }
    error.status = response.status
    throw error
  }
  return payload?.data as T
}

export function fetchFairUseMetrics(subscriptionID: number, signal?: AbortSignal) {
  return getFairUse<FairUseMetrics>(`/admin/subscriptions/${subscriptionID}/fair-use/metrics`, signal)
}

export function fetchFairUseObservations(subscriptionID: number, range: FairUseObservationRange, signal?: AbortSignal) {
  return getFairUse<FairUseObservationSeries>(`/admin/subscriptions/${subscriptionID}/fair-use/observations?range=${range}`, signal)
}

export function fetchFairUseState(subscriptionID: number, signal?: AbortSignal) {
  return getFairUse<FairUseState>(`/admin/subscriptions/${subscriptionID}/fair-use/state`, signal)
}

export function fetchFairUsePolicy(subscriptionID: number, signal?: AbortSignal) {
  return getFairUse<FairUsePolicyResolution>(`/admin/subscriptions/${subscriptionID}/fair-use/policy`, signal)
}

export function fetchFairUseEvents(subscriptionID: number, signal?: AbortSignal) {
  return getFairUse<FairUseEvent[]>(`/admin/subscriptions/${subscriptionID}/fair-use/events?limit=50`, signal)
}
