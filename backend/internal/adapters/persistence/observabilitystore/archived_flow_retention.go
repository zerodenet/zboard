package observabilitystore

import (
	"context"
	"fmt"
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, candidates[0].ID).Error; err != nil {
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
			var keys []string
			if err := tx.Model(&model.FlowCursorCheckpoint{}).Where("node_id = ?", node.ID).Order("flow_id").Limit(limit).Pluck("flow_id", &keys).Error; err != nil {
				return err
			}
			if len(keys) == 0 {
				// Terminal reporting fence is already checked under the same
				// exclusive node lock used by cold transfer. Remove at most
				// eight compressed floor pages, never accepted report claims.
				var pages []model.MeteringColdBlock
				if err := tx.Select("id,entry_count").Where("node_id=? AND kind=3", node.ID).Order("id").Limit(min(limit, 8)).Find(&pages).Error; err != nil {
					return err
				}
				if len(pages) == 0 {
					return nil
				}
				var root model.MeteringColdRoot
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND kind=3", node.ID).First(&root).Error; err != nil {
					return err
				}
				for _, page := range pages {
					if err := tx.Delete(&model.MeteringColdBlock{}, page.ID).Error; err != nil {
						return err
					}
					root.PageCount--
					root.EntryCount -= int64(page.EntryCount)
					count++
				}
				if root.PageCount < 0 || root.EntryCount < 0 {
					return fmt.Errorf("archived cold floor manifest is inconsistent")
				}
				return tx.Model(&root).Select("page_count", "entry_count").Updates(&root).Error
			}
			res := tx.Where("node_id = ? AND flow_id IN ?", node.ID, keys).Delete(&model.FlowCursorCheckpoint{})
			count = res.RowsAffected
			return res.Error
		}
		res := tx.Where("id IN ? AND node_id = ?", ids, node.ID).Delete(&model.FlowUsage{})
		count = res.RowsAffected
		return res.Error
	})
	return count, err
}

func archivedCursorNodes(db *gorm.DB, before time.Time) *gorm.DB {
	return db.Model(&model.Node{}).Where("lifecycle_status = ? AND archived_at < ? AND is_enabled = false AND node_credential_revoked_at IS NOT NULL AND traffic_secret_revoked_at IS NOT NULL", "archived", before).
		Where("EXISTS (?) OR EXISTS (?) OR EXISTS (?)", db.Model(&model.FlowUsage{}).Select("1").Where("flow_usages.node_id = nodes.id"), db.Model(&model.FlowCursorCheckpoint{}).Select("1").Where("flow_cursor_checkpoints.node_id = nodes.id"), db.Model(&model.MeteringColdRoot{}).Select("1").Where("metering_cold_roots.node_id=nodes.id AND kind=3 AND page_count>0"))
}
