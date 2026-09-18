package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// EnqueuePublication writes a domain intent consumed by the existing shared
// jobs runtime. Coalescing a newer generation must preserve an active lease.
func EnqueuePublication(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	now := time.Now().UTC()
	item := model.NodeConfigPublish{NodeID: nodeID, EndpointID: endpointID, RequestedBy: actor, Generation: 1, NextAttemptAt: now, LeaseUntil: time.Unix(0, 0).UTC()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}}, DoUpdates: clause.Assignments(map[string]any{"endpoint_id": endpointID, "requested_by": actor, "generation": gorm.Expr("generation + 1"), "attempts": 0, "last_error": "", "next_attempt_at": now, "updated_at": now})}).Create(&item).Error
}
