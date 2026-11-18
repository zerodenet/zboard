import type { ProtocolEndpointMutationResult } from '../api/client'

export function protocolEndpointMutationMessage(result: ProtocolEndpointMutationResult, copied = false): string {
  const membership = result.node_group_membership
  const membershipChanges = (membership?.added_node_group_ids?.length || 0) + (membership?.removed_node_group_ids?.length || 0)
  const taskIDs = membership?.reconcile_tasks?.map(task => `#${task.id}`).join('、') || ''
  if (membershipChanges > 0) {
    const endpointPublish = result.publish_status === 'queued'
    const membershipPublish = membership?.publish_status === 'queued'
    if (endpointPublish || membershipPublish) {
      if (copied) {
        return `协议已复制并关联 ${membershipChanges} 个节点组。任务${taskIDs ? ` ${taskIDs}` : '已排队'}，会先准备凭证，再发布到节点。应用确认后，订阅才会收到新入口。`
      }
      return `协议服务与 ${membershipChanges} 项节点组关联已保存；凭证协调任务 ${taskIDs || '已创建'}，受影响节点的发布请求已入队。`
    }
    return `协议服务与 ${membershipChanges} 项节点组关联已保存；凭证协调任务 ${taskIDs || '已创建'}，当前没有活跃订阅凭证需要发布节点。`
  }
  if (result.publish_status === 'queued') {
    return copied
      ? '协议已复制，等待目标节点应用配置。应用确认后，已关联的订阅才会收到新入口。'
      : '协议服务已保存，受影响节点的发布请求已入队。'
  }
  if (copied) return '协议配置已复制为独立服务；当前未启用，无需发布节点。'
  if (result.effect === 'delivery') return '协议服务已保存；交付配置已更新，无需发布节点。'
  if (result.effect === 'billing') return '协议服务已保存；新倍率将用于后续流量计费，无需发布节点。'
  if (result.effect === 'management') return '协议服务已保存；管理信息已更新，无需发布节点。'
  if (result.effect === 'runtime') return '协议服务的运行参数已保存；服务当前未启用，无需发布节点。'
  if (result.effect === 'credential_placement') return '协议服务的承载节点已更新；服务当前未启用，无需发布节点。'
  if (result.effect === 'none') return '协议服务已保存；没有检测到需要应用的变更。'
  return '协议服务已保存，无需发布节点。'
}
