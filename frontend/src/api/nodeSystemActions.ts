import axios from 'axios'
import { API_BASE, getAuthToken } from './client'
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

async function requestNodeSystemActions(nodeId: number, action?: 'enable_bbr'): Promise<NodeSystemActionsSnapshot> {
  try {
    const token = getAuthToken()
    const config = {
      timeout: 30_000,
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    }
    const response = action
      ? await axios.post(`${API_BASE}/nodes/${nodeId}/system-actions`, { action }, config)
      : await axios.get(`${API_BASE}/nodes/${nodeId}/system-actions`, config)
    return response.data?.data as NodeSystemActionsSnapshot
  } catch (cause: any) {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    throw cause
  }
}

export function fetchNodeSystemActions(nodeId: number): Promise<NodeSystemActionsSnapshot> {
  return requestNodeSystemActions(nodeId)
}

export function enableNodeBBR(nodeId: number): Promise<NodeSystemActionsSnapshot> {
  return requestNodeSystemActions(nodeId, 'enable_bbr')
}
