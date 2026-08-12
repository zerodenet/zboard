import axios from 'axios'
import { API_BASE, getAuthToken, type AdminTask } from './client'
import { normalizeApiErrorPayload } from '../utils/apiError'

export interface NodeBBRState {
  available: boolean
  active: boolean
  persistent: boolean
  congestion_control: string
  available_congestion_controls: string[]
  default_qdisc: string
  kernel_release: string
  sampled_at: string
  latency_ms: number
}

export interface NodeSystemActionsSnapshot {
  node_id: number
  supported_actions: string[]
  bbr: NodeBBRState
}

function nodeSystemActionConfig() {
  const token = getAuthToken()
  return {
    timeout: 30_000,
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  }
}

export async function fetchNodeSystemActions(nodeId: number): Promise<NodeSystemActionsSnapshot> {
  try {
    const response = await axios.get(`${API_BASE}/nodes/${nodeId}/system-actions`, nodeSystemActionConfig())
    return response.data?.data as NodeSystemActionsSnapshot
  } catch (cause: any) {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    throw cause
  }
}

export async function enableNodeBBR(nodeId: number): Promise<AdminTask> {
  try {
    const response = await axios.post(
      `${API_BASE}/nodes/${nodeId}/system-actions`,
      { action: 'enable_bbr' },
      nodeSystemActionConfig(),
    )
    return response.data?.data as AdminTask
  } catch (cause: any) {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    throw cause
  }
}
