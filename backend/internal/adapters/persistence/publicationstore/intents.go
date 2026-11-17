package publicationstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/credentialstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// EnqueuePublication writes a domain intent consumed by the existing shared
// jobs runtime. Coalescing a newer generation must preserve an active lease.
func Enqueue(tx *gorm.DB, nodeID, endpointID, actor uint) error {
	// A historical credential may still be revoked/reconciled after archive.
	// Retain that business change without recreating a current publication intent.
	var node model.Node
	result := tx.Clauses(credentialstore.CurrentResourceReadLock(tx)).Select("id,lifecycle_status").Where("id = ?", nodeID).Find(&node)
	if result.Error != nil {
		return credentialstore.CurrentResourceReadError(result.Error)
	}
	if result.RowsAffected == 0 || network.NodeManagementClosed(node.LifecycleStatus) {
		return nil
	}
	now := time.Now().UTC()
	item := model.NodeConfigPublish{NodeID: nodeID, EndpointID: endpointID, RequestedBy: actor, Generation: 1, NextAttemptAt: now, LeaseUntil: time.Unix(0, 0).UTC()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}}, DoUpdates: clause.Assignments(map[string]any{"endpoint_id": endpointID, "requested_by": actor, "generation": gorm.Expr("generation + 1"), "attempts": 0, "phase": "", "applied_config_sha256": "", "deferred": false, "suspended": false, "last_error": "", "next_attempt_at": now, "updated_at": now})}).Create(&item).Error
}
