import type {
  ProtocolEndpointNodeGroupMembership,
  ProtocolEndpointNodeGroupMembershipChange,
} from '../api/client'

export function buildProtocolNodeGroupMembershipChanges(
  original: ProtocolEndpointNodeGroupMembership[],
  desired: ProtocolEndpointNodeGroupMembership[],
): ProtocolEndpointNodeGroupMembershipChange[] {
  const originalByID = new Map(original.map(item => [item.node_group_id, item]))
  const desiredByID = new Map(desired.map(item => [item.node_group_id, item]))
  const changes: ProtocolEndpointNodeGroupMembershipChange[] = []

  for (const item of desired) {
    if (originalByID.has(item.node_group_id)) continue
    changes.push({
      node_group_id: item.node_group_id,
      expected_revision: item.revision,
      member: true,
    })
  }
  for (const item of original) {
    if (desiredByID.has(item.node_group_id)) continue
    changes.push({
      node_group_id: item.node_group_id,
      expected_revision: item.revision,
      member: false,
    })
  }
  return changes.sort((left, right) => left.node_group_id - right.node_group_id)
}
