import axios from 'axios'
import { API_BASE, getAuthToken, type AdminTask } from './client'
import { normalizeApiErrorPayload } from '../utils/apiError'
import { trackAdminTask, updateTrackedTask } from '../utils/taskTracker'

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

type NodeSystemActionTask = Omit<AdminTask, 'type'> & { type: 'node_system_action' }

function nodeSystemActionConfig() {
  const token = getAuthToken()
  return {
    timeout: 30_000,
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  }
}

function normalizeNodeSystemActionError(cause: any): never {
  if (cause?.response) {
    cause.response.data = normalizeApiErrorPayload(cause.response.data)
  } else if (cause instanceof Error) {
    cause.response = { data: { message: cause.message } }
  }
  throw cause
}

export async function fetchNodeSystemActions(nodeId: number): Promise<NodeSystemActionsSnapshot> {
  try {
    const response = await axios.get(`${API_BASE}/nodes/${nodeId}/system-actions`, nodeSystemActionConfig())
    return response.data?.data as NodeSystemActionsSnapshot
  } catch (cause: any) {
    return normalizeNodeSystemActionError(cause)
  }
}

async function fetchSystemActionTask(taskId: number): Promise<NodeSystemActionTask> {
  const response = await axios.get(`${API_BASE}/admin/tasks/${taskId}?summary=true`, nodeSystemActionConfig())
  return response.data?.data as NodeSystemActionTask
}

function taskFailure(task: NodeSystemActionTask): Error {
  const message = typeof task.errors === 'string' && task.errors.trim()
    ? task.errors.trim()
    : `VPS 自动化任务 #${task.id} 执行失败。`
  return new Error(message)
}

export async function enableNodeBBR(nodeId: number): Promise<NodeSystemActionsSnapshot> {
  try {
    const response = await axios.post(
      `${API_BASE}/nodes/${nodeId}/system-actions`,
      { action: 'enable_bbr' },
      nodeSystemActionConfig(),
    )
    const task = response.data?.data as NodeSystemActionTask
    trackAdminTask(task as AdminTask)

    const deadline = Date.now() + 60_000
    while (Date.now() < deadline) {
      const current = await fetchSystemActionTask(task.id)
      updateTrackedTask(current as AdminTask)
      if (current.status === 2) return fetchNodeSystemActions(nodeId)
      if (current.status === 3) throw taskFailure(current)
      await new Promise(resolve => setTimeout(resolve, 500))
    }
    throw new Error(`BBR 任务 #${task.id} 仍在后台执行，可在任务托盘查看最终结果。`)
  } catch (cause: any) {
    return normalizeNodeSystemActionError(cause)
  }
}
