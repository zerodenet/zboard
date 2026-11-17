package credentialstore

import (
	"fmt"
	"gorm.io/gorm"
	"time"
)

// Current membership is shared by authorization and transient material views.
// Forward access grants its landing authentication without exposing that direct
// landing address to the subscriber.
const CredentialMembershipSQL = `(SELECT node_group_id, protocol_endpoint_id FROM node_group_endpoints
UNION SELECT membership.node_group_id, entry.endpoint_id AS protocol_endpoint_id
FROM node_group_network_entries AS membership JOIN network_entries AS entry ON entry.id=membership.network_entry_id WHERE entry.enabled=true)`

// Material bindings exist only while rendering/reading. Legacy rows remain
// readable during compatibility and for historical accounting. No UNION branch
// writes an endpoint-sized credential or delivery state relation.
func Bindings(db *gorm.DB, at ...time.Time) *gorm.DB {
	columns := `c.material_group_id,c.material_version,c.material_scope,c.material_verifier,c.compatibility_started_at,c.compatibility_until,c.entitlement_epoch,c.current_scope,c.delivery_required,c.principal_digest,c.secret_principal_digest,c.principal_lookup_version,c.retired_principal_hash,c.id,c.subscription_id,c.user_id,%s AS protocol_endpoint_id,%s AS node_id,c.credential_id,c.principal_key,c.secret,%s AS listen_port,%s AS public_port,c.status,c.expires_at,c.last_used_at,c.revoked_at,c.created_at,c.updated_at`
	legacy := fmt.Sprintf(columns, "c.protocol_endpoint_id", "c.node_id", "c.listen_port", "c.public_port")
	root := fmt.Sprintf(columns, "e.id", "e.node_id", "e.port", "e.public_port")
	sql := "(SELECT " + legacy + " FROM protocol_credentials c WHERE c.material_version=0 AND (c.compatibility_until IS NULL OR c.compatibility_until>?) UNION ALL SELECT " + root + " FROM protocol_credentials c JOIN subscriptions s ON s.id=c.subscription_id JOIN " + CredentialMembershipSQL + " AS membership ON membership.node_group_id=s.node_group_id JOIN protocol_endpoints e ON e.id=membership.protocol_endpoint_id WHERE c.material_version=1 AND c.current_scope=1 AND c.material_scope=1 AND LOWER(e.protocol) IN ('vless','vmess','shadowsocks','trojan','hysteria2','mieru')) AS protocol_credentials"
	now := db.NowFunc().UTC()
	if len(at) > 0 {
		now = at[0]
	}
	return db.Table(sql, now)
}
