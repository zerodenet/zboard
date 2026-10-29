import { authenticatedAPI } from './client'

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
  let payload: any
  try {
    const response = await authenticatedAPI.get('/admin/system-info')
    payload = response.data
  } catch (cause: any) {
    if (cause?.response) cause.message = cause.response.data?.message || `系统信息请求失败（HTTP ${cause.response.status}）。`
    throw cause
  }
  if (!payload?.data) throw new Error('系统信息响应缺少 data。')
  return payload.data as AdminSystemInfo
}
