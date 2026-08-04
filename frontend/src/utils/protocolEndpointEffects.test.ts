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

  it('keeps the inactive-copy confirmation explicit', () => {
    expect(protocolEndpointMutationMessage(result({ effect: 'management', effects: ['management'] }), true))
      .toBe('协议配置已复制为独立服务；当前未启用，无需发布节点。')
  })
})
