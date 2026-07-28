import { describe, expect, it } from 'vitest'
import { adminActionLabel, adminActorLabel, auditTargetLabel, operationSummaryLabel, operationTargetLabel } from './adminDisplay'

describe('administrator semantic display labels', () => {
  it('translates known audit, kernel and task actions without losing unknown-value visibility', () => {
    expect(adminActionLabel('order.pay')).toBe('确认订单收款')
    expect(adminActionLabel('node.kernel.configure')).toBe('更新节点内核配置')
    expect(adminActionLabel('task.protocol_deploy')).toBe('批量发布协议配置')
    expect(adminActionLabel('future.action')).toBe('未知动作（future.action）')
  })

  it('formats actors and typed targets for operators', () => {
    expect(adminActorLabel('system')).toBe('系统')
    expect(adminActorLabel('admin@example.invalid')).toBe('admin@example.invalid')
    expect(auditTargetLabel('protocol_endpoint:42')).toBe('协议服务 #42')
    expect(auditTargetLabel('system_config:smtp_port')).toBe('系统配置 smtp_port')
    expect(operationTargetLabel({ target_type: 'task', target_id: 9 })).toBe('后台任务 #9')
  })

  it('converts persisted operation summaries into formatted domain output', () => {
    expect(operationSummaryLabel({ source: 'protocol_publish', summary: 'config revision 12000' })).toBe('配置版本 12,000')
    expect(operationSummaryLabel({ source: 'task', summary: 'progress 2500/10000; attempt 2/3' })).toBe('进度 2,500 / 10,000 · 尝试 2 / 3')
    expect(operationSummaryLabel({ source: 'node_kernel', summary: 'Zero 1.2.3 upgrade and passed systemd, control-socket, and panel-heartbeat health checks at 2026-07-24T00:00:00Z' })).toBe('Zero 1.2.3 已完成升级节点内核并通过健康检查')
    expect(operationSummaryLabel({ source: 'node_kernel', summary: 'Zero 0.0.15-rc.1 upgrade and passed systemd, control-socket, and connector-event health checks at 2026-07-26T00:00:00Z' })).toBe('Zero 0.0.15-rc.1 已完成升级节点内核并通过健康检查')
  })
})
