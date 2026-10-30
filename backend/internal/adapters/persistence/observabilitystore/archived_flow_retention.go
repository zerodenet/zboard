package observabilitystore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s HistoryRetention) pruneArchivedFlowCursors(ctx context.Context, before time.Time, limit int) (int64, error) {
	// Seek the archive index and node FK index, not the global flow table head.
	var candidates []model.Node
	if err := archivedCursorNodes(s.DB.WithContext(ctx), before).Select("nodes.id").Order("archived_at,id").Limit(1).Find(&candidates).Error; err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	var count int64
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&node, candidates[0].ID).Error; err != nil {
			return err
		}
		if node.LifecycleStatus != "archived" || node.ArchivedAt == nil || !node.ArchivedAt.Before(before) || node.IsEnabled || node.NodeCredentialRevokedAt == nil || node.TrafficSecretRevokedAt == nil {
			return nil
		}
		var ids []uint
		if err := tx.Model(&model.FlowUsage{}).Where("node_id = ?", node.ID).Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		res := tx.Where("id IN ? AND node_id = ?", ids, node.ID).Delete(&model.FlowUsage{})
		count = res.RowsAffected
		return res.Error
	})
	return count, err
}

func archivedCursorNodes(db *gorm.DB, before time.Time) *gorm.DB {
	return db.Model(&model.Node{}).Where("lifecycle_status = ? AND archived_at < ? AND is_enabled = false AND node_credential_revoked_at IS NOT NULL AND traffic_secret_revoked_at IS NOT NULL", "archived", before).
		Where("EXISTS (?)", db.Model(&model.FlowUsage{}).Select("1").Where("flow_usages.node_id = nodes.id"))
}
