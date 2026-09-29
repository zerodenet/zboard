package networkstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s NodeActivity) TryConnectorReceipt(ctx context.Context, nodeID uint, credential string, at time.Time) (applied bool, err error) {
	// MySQL stores these timestamps as DATETIME(3). Compare at that precision
	// so an earlier receipt cannot appear newer than a later stop rounded by DB.
	if s.DB.Dialector.Name() == "mysql" {
		at = at.Truncate(time.Millisecond)
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		// Skip a busy row rather than waiting behind accounting/publication. Lock
		// before validating credentials so rotation/revocation cannot race the write.
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Select("id", "is_enabled", "node_credential", "node_credential_revoked_at", "last_seen_at").Where("id = ?", nodeID).Find(&node)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// A nonlocking read distinguishes a deleted node from a busy row.
			var count int64
			if err := tx.Model(&model.Node{}).Where("id = ?", nodeID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return network.ErrNodeActivityCredential
			}
			return nil
		}
		if !node.IsEnabled || node.NodeCredential != credential || node.NodeCredentialRevokedAt != nil {
			return network.ErrNodeActivityCredential
		}
		applied = true
		if node.LastSeenAt != nil && !at.After(*node.LastSeenAt) {
			return nil
		}
		return tx.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]any{"connector_last_seen_at": at, "last_seen_at": at, "is_online": true, "status": 1}).Error
	})
	return applied, err
}
