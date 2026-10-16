import { authenticatedAPI as api } from './client'

export type RetirementMode = 'retire' | 'detach'
export interface NodeRetirement {
  node_id: number
  node_name: string
  mode: RetirementMode
  state: string
  run_id: string
  attempt: number
  last_error?: string
  steps: { kind: string; id: number; label: string; state: string; message?: string }[]
}
export async function fetchNodeRetirement(id: number, signal?: AbortSignal): Promise<NodeRetirement | null> {
  try { return (await api.get(`/admin/nodes/${id}/retirement`, { signal })).data.data }
  catch (cause: any) { if (cause?.response?.status === 404) return null; throw cause }
}
export async function startNodeRetirement(id: number, mode: RetirementMode, signal?: AbortSignal): Promise<NodeRetirement> {
  return (await api.post(`/admin/nodes/${id}/retirement`, { mode }, { signal })).data.data
}
