package commercestore

import (
	"context"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlanUpdate struct{ DB *gorm.DB }

func (s PlanUpdate) Update(ctx context.Context, actor, id uint, request commerce.PlanUpdateRequest) (commerce.Plan, error) {
	var out commerce.Plan
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := administrator(tx, actor)
		if err != nil {
			return err
		}
		var current model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			return resourceError(err)
		}
		candidate, fields, err := commerce.ApplyPlanUpdate(planView(current), request)
		if err != nil {
			return err
		}
		if request.NodeGroupID != nil || (candidate.IsActive && request.IsActive != nil) {
			exists, enabled, hasEndpoint, err := networkstore.CommerceGroupAvailability(tx, candidate.NodeGroupID)
			if err != nil {
				return err
			}
			if err := commerce.ValidatePlanGroup(candidate, request, exists, enabled, hasEndpoint); err != nil {
				return err
			}
		}
		if candidate.IsActive {
			rows, err := purchasableSKUs(tx, id, 0)
			if err != nil {
				return err
			}
			if err := commerce.ValidatePublication(true, len(rows)); err != nil {
				return err
			}
		}
		updated := planRow(candidate)
		fields = append(fields, "revision")
		result := tx.Model(&updated).Where("revision = ?", current.Revision).Select(fields).Updates(&updated)
		if result.Error != nil {
			return planWriteError(result.Error)
		}
		if result.RowsAffected != 1 {
			return &commerce.PlanRevisionError{Current: current.Revision}
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "plan.update", Target: fmt.Sprintf("plan:%d", id), Detail: fmt.Sprintf("fields=%d node_group=%d revision=%d", len(fields)-1, candidate.NodeGroupID, candidate.Revision)}).Error; err != nil {
			return err
		}
		if err := tx.Preload("SKUs").First(&updated, id).Error; err != nil {
			return err
		}
		out = planView(updated)
		return nil
	})
	if err != nil {
		return commerce.Plan{}, err
	}
	return out, nil
}
