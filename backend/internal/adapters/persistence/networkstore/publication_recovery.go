package networkstore

import (
	"context"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm/clause"
)

// RecoverUnqueuedPublications restores only missing durable work. Existing
// generations, leases, suspended requests and connection-revocation phases
// remain owned by their current queue entry. Each scan is bounded by node.
func (s PublicationQueue) RecoverUnqueuedPublications(ctx context.Context, now time.Time) (int, error) {
	var nodes []struct{ NodeID uint }
	err := s.DB.WithContext(ctx).Table("protocol_deployments d").
		Select("d.node_id").Joins("JOIN nodes n ON n.id = d.node_id").
		Where("n.archived_at IS NULL AND n.lifecycle_status NOT IN ? AND n.is_enabled = ?", []string{"archived", "deleting"}, true).
		Where("(d.status = ? OR (d.status = ? AND COALESCE(d.started_at,d.created_at) <= ?))", "reconciling", "running", now.Add(-network.PublicationLease)).
		Where("NOT EXISTS (SELECT 1 FROM node_config_publishes q WHERE q.node_id = d.node_id)").
		Group("d.node_id").Order("d.node_id").Limit(100).Scan(&nodes).Error
	if err != nil {
		return 0, err
	}
	count := 0
	for _, node := range nodes {
		row := model.NodeConfigPublish{NodeID: node.NodeID, Generation: 1, Reconciling: true, NextAttemptAt: now, LeaseUntil: publicationIdleLease}
		result := s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return count, result.Error
		}
		count += int(result.RowsAffected)
	}
	return count, nil
}
