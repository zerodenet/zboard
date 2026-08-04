import { describe, expect, it } from 'vitest'
import type { ProtocolEndpointMutationResult } from '../api/client'
import { protocolEndpointMutationMessage } from './protocolEndpointEffects'

function result(overrides: Partial<ProtocolEndpointMutationResult> = {}): ProtocolEndpointMutationResult {
  return {
    protocol_endpoint: {},
    effect: 'none',
    effects: [],
    publish_status: 'not_required',
    affected_node_ids: [],
    node_group_memberships: [],
    timing: {
      validation_ms: 0,
      transaction_ms: 0,
      task_enqueue_ms: 0,
      response_preparation_ms: 0,
      server_total_ms: 0,
    },
    ...overrides,
  }
}

describe('protocolEndpointMutationMessage', () => {
  it('distinguishes queued runtime publication from database-only saves', () => {
    expect(protocolEndpointMutationMessage(result({ effect: 'runtime', effects: ['runtime'], publish_status: 'queued', affected_node_ids: [3] })))
      .toContain('正在后台发布')
    expect(protocolEndpointMutationMessage(result({ effect: 'delivery', effects: ['delivery'] })))
      .toContain('无需发布节点')
    expect(protocolEndpointMutationMessage(result({ effect: 'billing', effects: ['billing'] })))
      .toContain('后续流量计费')
  })

  it('explains why inactive runtime and placement changes do not publish', () => {
    expect(protocolEndpointMutationMessage(result({ effect: 'runtime', effects: ['runtime'] })))
      .toContain('服务当前未启用')
    expect(protocolEndpointMutationMessage(result({ effect: 'credential_placement', effects: ['credential_placement'] })))
      .toContain('承载节点已更新')
  })

  it('describes incremental node-group reconciliation and publication', () => {
    expect(protocolEndpointMutationMessage(result({
      node_group_membership: { added_node_group_ids: [2], publish_status: 'queued', affected_node_ids: [3], reconcile_tasks: [{ id: 9 } as any] },
    }))).toContain('凭证协调任务 #9')
    expect(protocolEndpointMutationMessage(result({
      node_group_membership: { removed_node_group_ids: [2], publish_status: 'not_required', reconcile_tasks: [{ id: 10 } as any] },
    }))).toContain('没有活跃订阅凭证')
  })

  it('keeps the inactive-copy confirmation explicit', () => {
    expect(protocolEndpointMutationMessage(result({ effect: 'management', effects: ['management'] }), true))
      .toBe('协议配置已复制为独立服务；当前未启用，无需发布节点。')
  })
})
