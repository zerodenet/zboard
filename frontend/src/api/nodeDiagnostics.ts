import axios from 'axios'
import { API_BASE, getAuthToken } from './client'
import { normalizeApiErrorPayload } from '../utils/apiError'

export type NodeDiagnosticStatus = 'healthy' | 'error'
export type NodeDiagnosticReason =
  | 'ssh_unavailable'
  | 'zero_unavailable'
  | 'listener_unavailable'
  | 'config_invalid'
  | 'not_checked'
  | string

export interface NodeDiagnosticProtocol {
  name: string
  protocol: string
  status: NodeDiagnosticStatus
  reason?: NodeDiagnosticReason
}

export interface NodeDiagnosticSnapshot {
  node_id: number
  status: NodeDiagnosticStatus
  checks: {
    ssh: NodeDiagnosticStatus
    zero: NodeDiagnosticStatus
  }
  protocols: NodeDiagnosticProtocol[]
}

export async function runNodeDiagnostics(nodeId: number): Promise<NodeDiagnosticSnapshot> {
  try {
    const token = getAuthToken()
    const response = await axios.post(
      `${API_BASE}/nodes/${nodeId}/diagnostics`,
      {},
      {
        timeout: 30_000,
        headers: token ? { Authorization: `Bearer ${token}` } : undefined,
      },
    )
    return response.data?.data as NodeDiagnosticSnapshot
  } catch (cause: any) {
    if (cause?.response) cause.response.data = normalizeApiErrorPayload(cause.response.data)
    throw cause
  }
}
