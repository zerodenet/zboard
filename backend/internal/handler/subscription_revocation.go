package handler

import (
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Callers own the subscription transaction. Read only routing identities;
// revocation does not need to materialize credential secrets.
func persistCredentialRevocation(tx *gorm.DB, query *gorm.DB, status string, now time.Time) error {
	var credentials []model.ProtocolCredential
	if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "node_id", "protocol_endpoint_id").Find(&credentials).Error; err != nil {
		return err
	}
	if len(credentials) == 0 {
		return nil
	}
	sort.Slice(credentials, func(i, j int) bool {
		if credentials[i].NodeID == credentials[j].NodeID {
			return credentials[i].ProtocolEndpointID < credentials[j].ProtocolEndpointID
		}
		return credentials[i].NodeID < credentials[j].NodeID
	})
	var lastNode uint
	for _, credential := range credentials {
		if credential.NodeID == lastNode {
			continue
		}
		if err := enqueueNodeConfigPublish(tx, credential.NodeID, credential.ProtocolEndpointID, 0); err != nil {
			return err
		}
		lastNode = credential.NodeID
	}
	updates := map[string]interface{}{"status": status, "updated_at": now}
	if status == protocolCredentialStatusRevoked {
		updates["revoked_at"] = now
	}
	return tx.Model(&model.ProtocolCredential{}).Where("id IN ?", protocolCredentialIDs(credentials)).Updates(updates).Error
}

func revokeSubscriptionCredentialsOutsideGroup(tx *gorm.DB, sub model.Subscription, now time.Time) error {
	membership := tx.Model(&model.NodeGroupEndpoint{}).Select("protocol_endpoint_id").Where("node_group_id = ?", sub.NodeGroupID)
	query := tx.Model(&model.ProtocolCredential{}).
		Where("subscription_id = ? AND status IN ? AND protocol_endpoint_id NOT IN (?)", sub.ID,
			[]string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}, membership)
	return persistCredentialRevocation(tx, query, protocolCredentialStatusRevoked, now)
}

func expireSubscriptionsInTx(tx *gorm.DB, userID uint, now time.Time) error {
	query := tx.Model(&model.Subscription{}).
		Where("status = ? AND (end_at <= ? OR flow_used >= flow_total)", subStatusActive, now)
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Update("status", subStatusExpired).Error; err != nil {
		return err
	}
	inactive := tx.Model(&model.Subscription{}).Select("id").Where("status <> ?", subStatusActive)
	if userID != 0 {
		inactive = inactive.Where("user_id = ?", userID)
	}
	credentials := tx.Model(&model.ProtocolCredential{}).
		Where("status IN ? AND subscription_id IN (?)", []string{protocolCredentialStatusActive, protocolCredentialStatusPrepared}, inactive)
	return persistCredentialRevocation(tx, credentials, "expired", now)
}
