export interface DashboardStats {
  [key: string]: number | undefined
}

export type DashboardAttentionTone = 'warning' | 'danger'

export interface DashboardAttentionItem {
  key: 'tickets' | 'nodes' | 'deployments'
  label: string
  value: number
  icon: string
  tone: DashboardAttentionTone
  description: string
  to: string
}

export const dashboardActionRoutes = {
  tickets: '/admin/tickets?status=attention',
  nodes: '/admin/nodes?connector=offline',
  deployments: '/admin/protocols?deployment=failed',
} as const

function count(stats: DashboardStats, key: string): number {
  const value = Number(stats[key])
  return Number.isFinite(value) && value > 0 ? value : 0
}

/**
 * Build the operator inbox from conditions that are still unresolved now.
 *
 * Historical failed tasks/orders deliberately do not appear here. A failed
 * execution attempt is evidence for history/audit, not proof that the domain
 * objective is still unhealthy. Protocol deployments are safe to expose here
 * because the dashboard backend reports only endpoints whose latest deployment
 * is still failed, so a later successful deployment supersedes the failure.
 */
export function buildDashboardAttention(stats: DashboardStats): DashboardAttentionItem[] {
  const items: DashboardAttentionItem[] = []
  const pendingTickets = count(stats, 'tickets_pending')
  const offlineNodes = count(stats, 'nodes_offline')
  const unresolvedDeployments = count(stats, 'deployments_failed')

  if (offlineNodes > 0) {
    items.push({
      key: 'nodes',
      label: '离线节点',
      value: offlineNodes,
      icon: 'nodes',
      tone: 'danger',
      description: '当前可信运行信号已超过健康窗口',
      to: dashboardActionRoutes.nodes,
    })
  }

  if (unresolvedDeployments > 0) {
    items.push({
      key: 'deployments',
      label: '配置发布未恢复',
      value: unresolvedDeployments,
      icon: 'alert',
      tone: 'danger',
      description: '这些协议端点最新一次配置发布仍然失败',
      to: dashboardActionRoutes.deployments,
    })
  }

  if (pendingTickets > 0) {
    items.push({
      key: 'tickets',
      label: '待管理员回复工单',
      value: pendingTickets,
      icon: 'ticket',
      tone: 'warning',
      description: '当前工单状态仍在等待管理员处理',
      to: dashboardActionRoutes.tickets,
    })
  }

  return items
}
