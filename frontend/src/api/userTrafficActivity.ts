import { authenticatedAPI as api } from './client'

export interface UserTrafficDay { date: string; used_bytes: string }
export interface UserTrafficActivity { user_id: number; used_bytes: string; days: UserTrafficDay[] }
export interface UserTrafficActivityPage {
  from: string; to: string; timezone: string; as_of?: string; projection_pending: boolean; items: UserTrafficActivity[]
}
export async function fetchUserTrafficActivity(users: number[], signal?: AbortSignal): Promise<UserTrafficActivityPage> {
  const { data } = await api.get('/admin/traffic/user-activity', { params: { user_ids: users.join(',') }, signal })
  const value = data.data as UserTrafficActivityPage
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value.from) || !/^\d{4}-\d{2}-\d{2}$/.test(value.to) || !Array.isArray(value.items)) throw new Error('流量统计响应不完整。')
  if (!value.projection_pending) {
    const ids = new Set(users)
    if (value.items.length !== ids.size || new Set(value.items.map(item => item.user_id)).size !== ids.size) throw new Error('流量统计缺少用户。')
    const dates = Array.from({length:7}, (_, index) => {
      const date = new Date(value.from + 'T00:00:00Z'); date.setUTCDate(date.getUTCDate() + index); return date.toISOString().slice(0,10)
    })
    if (dates[6] !== value.to) throw new Error('流量统计日期无效。')
    for (const item of value.items) {
      if (!ids.has(item.user_id) || (typeof item.used_bytes !== 'string' || !/^\d+$/.test(item.used_bytes)) || !Array.isArray(item.days) || item.days.length !== 7 || item.days.some((day, index) => day.date !== dates[index] || (typeof day.used_bytes !== 'string' || !/^\d+$/.test(day.used_bytes))) || item.days.reduce((sum,day) => sum + BigInt(day.used_bytes),0n) !== BigInt(item.used_bytes)) throw new Error('流量统计数据不完整。')
    }
  }
  return value
}
