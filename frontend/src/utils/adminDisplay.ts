import type { OperationLog } from '../api/client'
import { formatNumber, formatUnknownValue } from './format'
import { normalizeOutput, truncateOutput } from './output'

const auditActions: Record<string, string> = {
  'system.settings.update': '更新站点设置',
  'system.config.update': '更新系统配置',
  'user.create': '创建用户',
  'user.update': '更新用户',
  'node.create': '登记节点',
  'node.update': '更新节点',
  'node.ssh_config.update': '更新节点 SSH 配置',
  'node.ssh_host_key.reset': '重置信任的 SSH 主机密钥',
  'node.ssh_terminal.open': '打开 SSH 终端',
  'node.ssh_terminal.close': '关闭 SSH 终端',
  'node.connector_credential.rotate': '轮换节点连接凭证',
  'node.connector_credential.revoke': '吊销节点连接凭证',
  'node.traffic_credential.rotate': '轮换节点流量凭证',
  'node.traffic_credential.revoke': '吊销节点流量凭证',
  'node.lifecycle.batch': '批量变更节点生命周期',
  'protocol_endpoint.create': '创建协议服务',
  'protocol_endpoint.update': '更新协议服务',
  'protocol_endpoint.multiplier.update': '更新协议计费倍率',
  'protocol_endpoint.active.batch': '批量启停协议服务',
  'node_group.create': '创建节点组',
  'node_group.update': '更新节点组',
  'plan.create': '创建商品',
  'plan.update': '更新商品',
  'plan.sku.create': '创建销售规格',
  'plan.sku.update': '更新销售规格',
  'order.pay': '确认订单收款',
  'order.cancel': '取消订单',
  'order.payment_result': '处理支付结果',
  'subscription_token.revoke': '吊销订阅凭证',
  'subscription_token.rotate': '轮换订阅凭证',
  'subscription_template.create': '创建订阅模板',
  'subscription_template.update': '更新订阅模板',
  'subscription_template.delete': '删除订阅模板',
  'subscription_rule_set.create': '创建规则集',
  'subscription_rule_set.update': '更新规则集',
  'subscription_rule_set.delete': '删除规则集',
  'ticket.status.update': '更新工单状态',
  'task.create': '创建后台任务',
  'task.run': '执行后台任务',
}

const kernelActions: Record<string, string> = {
  detect: '检测节点内核',
  reconcile: '对齐节点内核',
  install: '安装节点内核',
  upgrade: '升级节点内核',
  repair: '修复节点内核',
  configure: '更新节点内核配置',
  manual_review: '人工复核节点内核',
  none: '确认节点内核无需变更',
}

const taskActions: Record<string, string> = {
  quota: '调整订阅配额',
  email: '发送邮件通知',
  node_detect: '批量检测节点',
  node_reconcile: '批量对齐节点内核',
  node_lifecycle: '批量变更节点生命周期',
  protocol_deploy: '批量发布协议配置',
  protocol_active: '批量启停协议服务',
  node_group_reconcile: '对齐节点组交付',
}

const targetTypes: Record<string, string> = {
  installation: '站点安装配置',
  system_config: '系统配置',
  user: '用户',
  node: '节点',
  protocol_endpoint: '协议服务',
  node_group: '节点组',
  plan: '商品',
  plan_sku: '销售规格',
  order: '订单',
  subscription_template: '订阅模板',
  subscription_rule_set: '规则集',
  ticket: '工单',
  task: '后台任务',
}

export function adminActionLabel(value: string) {
  if (auditActions[value]) return auditActions[value]
  const kernelMatch = /^node\.kernel\.(.+)$/.exec(value)
  if (kernelMatch) return kernelActions[kernelMatch[1]] || formatUnknownValue('节点内核动作', kernelMatch[1])
  const taskMatch = /^task\.(.+)$/.exec(value)
  if (taskMatch) return taskActions[taskMatch[1]] || formatUnknownValue('任务动作', taskMatch[1])
  if (value === 'protocol.publish') return '发布协议配置'
  return formatUnknownValue('动作', value)
}

export function auditTargetLabel(value: string) {
  const match = /^([a-z_]+):(.+)$/.exec(value || '')
  if (!match) return value ? formatUnknownValue('目标', value) : '未记录目标'
  const type = targetTypes[match[1]]
  if (!type) return formatUnknownValue('目标类型', match[1])
  const identifier = match[2]
  return /^\d+$/.test(identifier) ? `${type} #${identifier}` : `${type} ${identifier}`
}

export function adminActorLabel(value: string) {
  return !value || value === 'system' ? '系统' : value
}

export function operationTargetLabel(item: Pick<OperationLog, 'target_type' | 'target_id' | 'node_id' | 'protocol_endpoint_id'>) {
  if (item.protocol_endpoint_id) return `协议服务 #${item.protocol_endpoint_id}`
  if (item.node_id) return `节点 #${item.node_id}`
  const type = targetTypes[item.target_type] || formatUnknownValue('目标类型', item.target_type)
  return `${type} #${item.target_id}`
}

export function operationSummaryLabel(item: Pick<OperationLog, 'source' | 'summary'>) {
  const summary = normalizeOutput(item.summary)
  if (!summary) return '未记录摘要'
  const protocol = /^config revision (\d+)$/.exec(summary)
  if (item.source === 'protocol_publish' && protocol) return `配置版本 ${formatNumber(Number(protocol[1]))}`
  const task = /^progress (\d+)\/(\d+);\s*attempt (\d+)\/(\d+)$/.exec(summary)
  if (item.source === 'task' && task) {
    return `进度 ${formatNumber(Number(task[1]))} / ${formatNumber(Number(task[2]))} · 尝试 ${formatNumber(Number(task[3]))} / ${formatNumber(Number(task[4]))}`
  }
  if (summary === 'Zero is already at the desired binary and configuration') return 'Zero 已达到目标版本与配置'
  const kernel = /^Zero (\S+) (\S+) and passed systemd, control-socket, and panel-heartbeat health checks(?: at .+)?$/.exec(summary)
  if (item.source === 'node_kernel' && kernel) {
    const action = kernelActions[kernel[2]] || formatUnknownValue('节点内核动作', kernel[2])
    return `Zero ${kernel[1]} 已完成${action}并通过健康检查`
  }
  return truncateOutput(summary, 300)
}
