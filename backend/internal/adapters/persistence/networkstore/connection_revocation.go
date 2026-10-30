package networkstore

import (
	"context"
	"crypto/subtle"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type ConnectionRevocation struct {
	DB     *gorm.DB
	Cipher metering.CredentialDecryptor
}

func (s ConnectionRevocation) WithRevokedPrincipal(ctx context.Context, nodeID uint, principal string, close func() error) (forbidden bool, err error) {
	if s.DB == nil || nodeID == 0 || principal == "" || close == nil {
		return false, network.ErrConnectionRevocationPending
	}
	candidate, err := (credentialstore.PrincipalIndex{DB: s.DB, Cipher: s.Cipher}).Lookup(ctx, nodeID, principal)
	if err != nil {
		return false, network.ErrConnectionRevocationPending
	}

	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Commerce, expiry, quota and credential reconciliation use subscription
		// before credential lock order. Recheck after acquiring both locks.
		if tx.Dialector.Name() == "sqlite" {
			if err := tx.Model(&model.Subscription{}).Where("id = ?", candidate.SubscriptionID).UpdateColumn("kernel_policy_revision", gorm.Expr("kernel_policy_revision")).Error; err != nil {
				return err
			}
		}
		// Infrastructure mutations lock the node before endpoint/group changes.
		// SHARE remains compatible with commerce's credential foreign-key checks;
		// an exclusive node lock here would invert commerce's subscription order.
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "lifecycle_status", "is_enabled").First(&node, nodeID).Error; err != nil || network.NodeManagementClosed(node.LifecycleStatus) {
			return network.ErrConnectionRevocationPending
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, candidate.SubscriptionID).Error; err != nil {
			return network.ErrConnectionRevocationPending
		}
		var credential model.ProtocolCredential
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&credential, candidate.ID).Error; err != nil {
			return network.ErrConnectionRevocationPending
		}
		if credential.SubscriptionID != sub.ID || !s.matches(nodeID, principal, credential) {
			return network.ErrConnectionRevocationPending
		}
		now := time.Now().UTC()
		forbidden = credential.EntitlementEpoch != sub.EntitlementEpoch || credential.CurrentScope == nil || *credential.CurrentScope != 1 || !node.IsEnabled || sub.Status != "active" || !sub.EndAt.After(now) || sub.FlowUsed >= sub.FlowTotal
		if !forbidden {
			// Credential reconciliation is asynchronous. A revoked projection may
			// predate a regrant, so current group membership is authoritative. The
			// group lock serializes a membership commit with the native command.
			var group model.NodeGroup
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").First(&group, sub.NodeGroupID).Error; err != nil {
				return network.ErrConnectionRevocationPending
			}
			var granted int64
			if err := tx.Model(&model.ProtocolEndpoint{}).
				Joins(CredentialMembershipJoin("node_group_endpoints.protocol_endpoint_id = protocol_endpoints.id")).
				Where("node_group_endpoints.node_group_id = ? AND protocol_endpoints.id = ? AND protocol_endpoints.node_id = ? AND protocol_endpoints.is_active = ?", sub.NodeGroupID, credential.ProtocolEndpointID, nodeID, true).
				Count(&granted).Error; err != nil {
				return err
			}
			forbidden = granted == 0
			if !forbidden && (credential.Status != "active" || credential.RevokedAt != nil || !credential.ExpiresAt.After(now)) {
				return network.ErrConnectionRevocationPending
			}
		}
		if !forbidden {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return close()
	})
	return forbidden, err
}
func (s ConnectionRevocation) matches(nodeID uint, principal string, row model.ProtocolCredential) bool {
	if row.PrincipalKey != "" && row.PrincipalKey == principal {
		return true
	}
	if row.RetiredPrincipalHash != "" && row.RetiredPrincipalHash == entitlements.RetiredPrincipalHash(nodeID, principal) {
		return true
	}
	if row.Secret == "" || s.Cipher == nil {
		return false
	}
	plain, err := s.Cipher.Decrypt(row.Secret)
	return err == nil && len(plain) == len(principal) && subtle.ConstantTimeCompare([]byte(plain), []byte(principal)) == 1
}
