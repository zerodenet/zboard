package commercestore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SKUUpdate struct{ DB *gorm.DB }

func (s SKUUpdate) Update(ctx context.Context, actor, id uint, in commerce.NormalizedSKU) (commerce.SKU, error) {
	var out commerce.SKU
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := administrator(tx, actor)
		if err != nil {
			return err
		}
		// Read only parent identity before locking. SKU parentage is immutable.
		var identity model.PlanSKU
		if err := tx.Select("id", "plan_id").First(&identity, id).Error; err != nil {
			return resourceError(err)
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "is_active").First(&plan, identity.PlanID).Error; err != nil {
			return resourceError(err)
		}
		var existing model.PlanSKU
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND plan_id = ?", id, plan.ID).First(&existing).Error; err != nil {
			return resourceError(err)
		}
		others, err := purchasableSKUs(tx, plan.ID, id)
		if err != nil {
			return err
		}
		if err := commerce.ValidateSKUAvailability(plan.IsActive, len(others), in); err != nil {
			return err
		}
		candidate := model.PlanSKU(in.SKU)
		candidate.ID, candidate.PlanID, candidate.CreatedAt = existing.ID, existing.PlanID, existing.CreatedAt
		fields := []string{"code", "name", "sku_type", "billing_mode", "entitlement_mode", "renewal_effect", "billing_unit", "billing_value", "price_cents", "currency", "traffic_bytes", "device_limit", "speed_limit_mbps", "is_active", "sort_order"}
		if err := tx.Model(&candidate).Select(fields).Updates(&candidate).Error; err != nil {
			return skuWriteError(err)
		}
		if err := tx.Where("plan_sku_id = ?", id).Delete(&model.PlanSKUOperation{}).Error; err != nil {
			return err
		}
		for _, operation := range in.AllowedOperations {
			if err := tx.Create(&model.PlanSKUOperation{PlanSKUID: id, Operation: operation}).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "plan.sku.update", Target: fmt.Sprintf("plan_sku:%d", id), Detail: fmt.Sprintf("code=%s operations=%s", candidate.Code, strings.Join(in.AllowedOperations, ","))}).Error; err != nil {
			return err
		}
		// Return the committed projection including database-managed timestamps.
		if err := tx.First(&candidate, id).Error; err != nil {
			return err
		}
		out = commerce.SKU(candidate)
		return nil
	})
	if err != nil {
		return commerce.SKU{}, err
	}
	return out, nil
}
func resourceError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commerce.ErrNotFound
	}
	return err
}
func administrator(tx *gorm.DB, actor uint) (model.User, error) {
	var user model.User
	result := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "email").Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").Limit(1).Find(&user)
	if result.Error != nil {
		return user, result.Error
	}
	if result.RowsAffected != 1 {
		return user, commerce.ErrPermission
	}
	return user, nil
}

// Locking reads avoid a stale MySQL REPEATABLE READ snapshot after waiting for
// the parent lock. Every SKU mutation takes that parent lock first.
func purchasableSKUs(tx *gorm.DB, planID, exclude uint) ([]model.PlanSKU, error) {
	var rows []model.PlanSKU
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("plan_skus.id").
		Joins("JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?", "purchase").
		Where("plan_skus.plan_id = ? AND plan_skus.id <> ? AND plan_skus.is_active = ?", planID, exclude, true).Find(&rows).Error
	return rows, err
}
