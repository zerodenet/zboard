import { authenticatedAPI as api } from './client'
export interface RuntimeJob {
  id: string; name: string; kind: string; interval_seconds: number; concurrency: number
  state: string; running: number; runs: number; failures: number; missed_runs?: number; timezone?: string; misfire_policy?: string; last_started_at: string | null
  last_finished_at: string | null; last_duration_ms: number; last_result: string; last_error: string; next_scan_at: string | null
}
export interface PluginTask extends RuntimeJob { plugin_id: string; plugin_name: string; task_id: string; timeout_seconds: number }
export interface RuntimeQueue { oldest_at?: string | null; id: string; name: string; pending: number; running: number; delayed: number; stale: number; drafts: number; failed: number }
export interface QueueItem { id: number; kind: string; state: string; attempts: number; created_at: string; next_attempt_at: string | null; lease_until: string | null; last_error: string }
export interface QueuePage { items: QueueItem[]; total: number; offset: number; limit: number }
export interface RuntimeJobs {
  as_of: string; started_at: string; observation_scope: string; jobs: RuntimeJob[]; queues: RuntimeQueue[]
  execution_queue?: { pending: number; running: number; delayed: number; unknown: number; maintenance_reserved?: boolean; pending_limit?: number; plugin_pending_limit?: number; plugin_owner_pending_limit?: number }; execution_concurrency?: number; external_execution_concurrency?: number; admin_task_concurrency: number; admin_item_concurrency: number
  plugin_tasks?: PluginTask[] | null; plugin_task_concurrency?: number
  plugin_host: { state: string; epoch: number; lease_until: string | null; renewal_failures: number; interval_seconds: number } | null
  runtime: { event_spool: { pending_events: number; pending_bytes: number; oldest_event_at?: string; pressure_level: string } | null }
}
export async function fetchRuntimeJobs(signal?: AbortSignal): Promise<RuntimeJobs> { return (await api.get('/admin/runtime-jobs', { signal })).data.data }
export async function fetchRuntimeQueue(name: string, offset: number, signal?: AbortSignal): Promise<QueuePage> { return (await api.get('/admin/runtime-jobs/queue', { params: { name, offset, limit: 25 }, signal })).data.data }
