import { API_BASE, getAuthToken } from './client'

export interface AdminProjectLink {
  label: string
  url: string
}

export interface AdminSystemInfo {
  service: string
  version: string
  release_version: string
  commit: string
  build_time: string
  release_channel: string
  started_at: string
  uptime_seconds: number
  installed_at: string
  license: {
    spdx: string
    name: string
    edition: string
  }
  links: AdminProjectLink[]
  update_url: string
}

export async function fetchAdminSystemInfo(): Promise<AdminSystemInfo> {
  const token = getAuthToken()
  const response = await fetch(`${API_BASE}/admin/system-info`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    throw new Error(payload?.message || `系统信息请求失败（HTTP ${response.status}）。`)
  }
  if (!payload?.data) throw new Error('系统信息响应缺少 data。')
  return payload.data as AdminSystemInfo
}
