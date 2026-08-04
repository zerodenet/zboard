import type { ProtocolEndpointMutationResult } from '../api/client'

export function protocolEndpointMutationMessage(result: ProtocolEndpointMutationResult, copied = false): string {
  if (result.publish_status === 'queued') {
    return copied
      ? '协议配置已复制为独立服务，受影响节点的运行配置正在后台发布。'
      : '协议服务已保存，受影响节点的运行配置正在后台发布。'
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
