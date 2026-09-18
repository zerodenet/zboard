import { authenticatedAPI as api } from './client'
export interface RunRecord { id: string; owner: string; handler: string; state: string; attempts?: number; max_attempts?: number; next_attempt_at?: string; planned_at?: string; created_at: string; finished_at: string | null }
export interface RunHistory { items: RunRecord[]; total: number; offset: number; limit: number }
export interface RunAttempt { attempt_number: number; worker: string; state: string; started_at: string; expires_at: string; finished_at: string | null }
export interface RunAttempts { items: RunAttempt[]; total: number; offset: number; limit: number }
export async function fetchRunHistory(offset: number, signal?: AbortSignal): Promise<RunHistory> {
  return (await api.get('/admin/runtime-jobs/runs', { params: { offset, limit: 25 }, signal })).data.data
}
export async function fetchRunAttempts(id: string, signal?: AbortSignal): Promise<RunAttempts> {
  return (await api.get(`/admin/runtime-jobs/runs/${encodeURIComponent(id)}/attempts`, { params: { offset: 0, limit: 25 }, signal })).data.data
}
export async function cancelRun(id: string, reason: string): Promise<string> {
  return (await api.post(`/admin/runtime-jobs/runs/${encodeURIComponent(id)}/cancel`, { reason })).data.data.state
}
export async function resolveRun(id: string, outcome: string, reason: string): Promise<void> {
  await api.post(`/admin/runtime-jobs/runs/${encodeURIComponent(id)}/resolve`, { outcome, reason })
}

export async function reconcileDNSRun(id: string): Promise<void> {
  await api.post(`/admin/runtime-jobs/runs/${encodeURIComponent(id)}/reconcile-dns`)
}
