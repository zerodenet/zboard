package networkstore

// CredentialMembershipSQL grants authentication on a protocol either directly
// or through an enabled forward entry. UNION deduplicates shared credentials.
// Delivery must still use the original memberships: a forward grant does not
// expose the landing protocol's direct address to the subscriber.
const CredentialMembershipSQL = `(SELECT node_group_id, protocol_endpoint_id FROM node_group_endpoints
UNION SELECT membership.node_group_id, entry.endpoint_id AS protocol_endpoint_id
FROM node_group_network_entries AS membership
JOIN network_entries AS entry ON entry.id = membership.network_entry_id
WHERE entry.enabled = true)`

func CredentialMembershipJoin(condition string) string {
	return "JOIN " + CredentialMembershipSQL + " AS node_group_endpoints ON " + condition
}
