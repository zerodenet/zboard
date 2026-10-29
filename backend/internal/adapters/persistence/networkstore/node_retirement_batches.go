package networkstore

import (
	"context"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const retirementBatchSize = 256

// The final resource transaction must not cascade through a node's lifetime of
// flow cursors and ledger references while holding the global job admission
// lock. Each committed batch is fenced by the actual retirement lease. The
// deleting lifecycle rejects new accounting before cursors can be retired;
// traffic rows, their financial fields and immutable report receipts survive.
func (s NodeRetirement) prepareRemoval(ctx context.Context, id uint, runID string) error {
	for _, stage := range []string{"flow_cursors", "traffic_node", "traffic_endpoint"} {
		for {
			done, err := s.removalBatch(ctx, id, runID, stage)
			if err != nil {
				return err
			}
			if done {
				break
			}
		}
	}
	return nil
}

func (s NodeRetirement) removalBatch(ctx context.Context, id uint, runID, stage string) (done bool, err error) {
	// This execution-only transaction does not change admission or submit work.
	// The retirement and its current Run are locked until commit, fencing cancel,
	// expiry and retry without taking the global job admission mutex. Never call
	// a ledger/admission writer while these domain locks are held.
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := retirementLease(tx, id, runID)
		if err != nil {
			return err
		}
		for _, step := range retirementView(row).Steps {
			if step.State != "succeeded" && step.State != "retained" {
				return jobs.ErrConflict
			}
		}
		admin, err := providerAdmin(tx, row.ActorID)
		if err != nil {
			return err
		}
		var node model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&node, id).Error; err != nil {
			return err
		}
		if node.LifecycleStatus != "deleting" || node.IsEnabled {
			return jobs.ErrConflict
		}
		var endpoints []uint
		if err := tx.Model(&model.ProtocolEndpoint{}).Where("node_id = ?", id).Pluck("id", &endpoints).Error; err != nil {
			return err
		}
		blockers, err := nodeRemovalBlockers(tx, id, endpoints)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return &network.ResourceRemovalBlocked{Blockers: blockers}
		}
		var value any = &model.TrafficRecord{}
		column := "node_id"
		query := tx.Where("node_id = ?", id)
		switch stage {
		case "flow_cursors":
			value = &model.FlowUsage{}
		case "traffic_node":
		case "traffic_endpoint":
			if len(endpoints) == 0 {
				done = true
				return nil
			}
			column = "protocol_endpoint_id"
			query = tx.Where("protocol_endpoint_id IN ?", endpoints)
		default:
			return jobs.ErrInvalid
		}
		var ids []uint
		if err := query.Model(value).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(retirementBatchSize).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			done = true
			return nil
		}
		batch := tx.Model(value).Where("id IN ?", ids)
		var result *gorm.DB
		if stage == "flow_cursors" {
			result = batch.Delete(value)
		} else {
			// Preserve both GORM and legacy MySQL ON UPDATE timestamps.
			result = batch.UpdateColumns(map[string]any{column: nil, "updated_at": gorm.Expr("updated_at")})
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return jobs.ErrConflict
		}
		return tx.Create(&model.AuditLog{UserID: &admin.ID, Actor: admin.Email, Action: "node.retirement.batch", Target: fmt.Sprintf("node:%d", id), Detail: fmt.Sprintf("run=%s stage=%s rows=%d", runID, stage, len(ids))}).Error
	})
	return
}
