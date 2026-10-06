import { api } from './client'
import type { AdminTask } from './client'

export async function uploadNodeKernel(nodeId: number, file: File, version: string, allowDowngrade: boolean, key: string, progress: (percent: number) => void): Promise<AdminTask> {
  const data = new FormData()
  data.append('file', file)
  data.append('version', version)
  data.append('allow_downgrade', String(allowDowngrade))
  data.append('idempotency_key', key)
  const response = await api.post(`/nodes/${nodeId}/kernel/upload`, data, {
    timeout: 180_000,
    onUploadProgress: event => { if (event.total) progress(Math.min(100, Math.round(event.loaded / event.total * 100))) },
  })
  return response.data.data
}
