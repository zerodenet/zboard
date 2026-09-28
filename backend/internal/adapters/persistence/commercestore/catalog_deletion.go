package commercestore

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Archive catalog entries while retaining order and subscription references.
type CatalogDeletion struct{ DB *gorm.DB }

func (s CatalogDeletion) DeletePlan(ctx context.Context, actor, id uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := administrator(tx, actor)
		if err != nil {
			return err
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, id).Error; err != nil {
			return resourceError(err)
		}
		if plan.ArchivedAt != nil {
			return nil
		}
		now := time.Now().UTC()
		if err := tx.Model(&model.PlanSKU{}).Where("plan_id = ? AND archived_at IS NULL", id).Updates(map[string]any{"archived_at": now, "is_active": false}).Error; err != nil {
			return err
		}
		if err := tx.Model(&plan).Updates(map[string]any{"archived_at": now, "is_active": false, "revision": plan.Revision + 1}).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "plan.delete", Target: fmt.Sprintf("plan:%d", id), Detail: "catalog archived; existing orders and subscriptions retained"}).Error
	})
}

func (s CatalogDeletion) DeleteSKU(ctx context.Context, actor, id uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := administrator(tx, actor)
		if err != nil {
			return err
		}
		var identity model.PlanSKU
		if err := tx.Select("id", "plan_id").First(&identity, id).Error; err != nil {
			return resourceError(err)
		}
		// All SKU mutations lock their parent before the SKU itself.
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, identity.PlanID).Error; err != nil {
			return resourceError(err)
		}
		var sku model.PlanSKU
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, id).Error; err != nil {
			return resourceError(err)
		}
		if sku.ArchivedAt != nil {
			return nil
		}
		others, err := purchasableSKUs(tx, plan.ID, id)
		if err != nil {
			return err
		}
		if err := tx.Model(&sku).Updates(map[string]any{"archived_at": time.Now().UTC(), "is_active": false}).Error; err != nil {
			return err
		}
		updates := map[string]any{"revision": plan.Revision + 1}
		if len(others) == 0 {
			updates["is_active"] = false
		}
		if err := tx.Model(&plan).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "plan.sku.delete", Target: fmt.Sprintf("plan_sku:%d", id), Detail: fmt.Sprintf("plan=%d catalog archived; history retained", plan.ID)}).Error
	})
}
