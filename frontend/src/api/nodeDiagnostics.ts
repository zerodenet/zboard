import axios from 'axios'
import { API_BASE, getAuthToken } from './client'
import { normalizeApiErrorPayload } from '../utils/apiError'

export type NodeDiagnosticClassification = 'healthy' | 'data_plane_missing' | 'network_reachability' | 'resource_pressure' | 'unknown'

export interface NodeDiagnosticExpectedListener {
  tag: string
  protocol: string
  address: string
  port: number
  networks: Array<'tcp' | 'udp' | string>
  present: boolean
  missing_networks?: string[]
  external_reachability: 'reachable' | 'unreachable' | 'not_checked' | string
}

export interface NodeDiagnosticActualListener {
  network: 'tcp' | 'udp' | string
  state: string
  address: string
  port: number
}

export interface NodeDiagnosticSnapshot {
  node_id: number
  captured_at: string
  latency_ms: number
  classification: NodeDiagnosticClassification
  summary: string
  runtime: {
    control_status: string
    core_instance_id?: string
    config_revision?: number
    pid?: number
    config_path?: string
    engine_version?: string
    started_at_unix_ms?: number
    listener_health_source: string
  }
  service: {
    active_state?: string
    sub_state?: string
    main_pid?: number
    exec_main_status?: number
  }
  resources: {
    fd_count?: number
    fd_soft_limit?: number
    fd_ratio?: number
    conntrack_count?: number
    conntrack_max?: number
    conntrack_ratio?: number
    resource_pressure: boolean
    resource_information_available: boolean
  }
  expected_listeners: NodeDiagnosticExpectedListener[]
  actual_listeners: NodeDiagnosticActualListener[]
  recent_zero_logs?: string
  recent_kernel_logs?: string
  capabilities: {
    ssh: boolean
    native_runtime_snapshot: boolean
    native_listener_health: boolean
    tcp_external_reachability: boolean
    udp_external_reachability: boolean
  }
  warnings?: string[]
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
