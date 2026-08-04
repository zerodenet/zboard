import { describe, expect, it } from 'vitest'
import type { ProtocolEndpointNodeGroupMembership } from '../api/client'
import { buildProtocolNodeGroupMembershipChanges } from './protocolNodeGroupMembership'

function membership(node_group_id: number, revision: number): ProtocolEndpointNodeGroupMembership {
  return { node_group_id, revision, name: `Group ${node_group_id}`, code: `group-${node_group_id}`, description: '', is_enabled: true, sort_order: 0 }
}

describe('protocol node-group membership changes', () => {
  it('sends only incremental additions and removals with the loaded group revisions', () => {
    expect(buildProtocolNodeGroupMembershipChanges(
      [membership(1, 3), membership(2, 7)],
      [membership(2, 7), membership(3, 11)],
    )).toEqual([
      { node_group_id: 1, expected_revision: 3, member: false },
      { node_group_id: 3, expected_revision: 11, member: true },
    ])
  })

  it('does not turn unchanged memberships into a full replacement command', () => {
    expect(buildProtocolNodeGroupMembershipChanges([membership(1, 4)], [membership(1, 4)])).toEqual([])
  })
})
