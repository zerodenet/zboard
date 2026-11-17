package networkstore

import "github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"

// CredentialMembershipSQL grants authentication on a protocol either directly
// or through an enabled forward entry. UNION deduplicates shared credentials.
// Delivery must still use the original memberships: a forward grant does not
// expose the landing protocol's direct address to the subscriber.
const CredentialMembershipSQL = credentialstore.CredentialMembershipSQL

func CredentialMembershipJoin(condition string) string {
	return "JOIN " + CredentialMembershipSQL + " AS node_group_endpoints ON " + condition
}
