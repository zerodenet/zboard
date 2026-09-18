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

type SKUCreation struct{ DB *gorm.DB }

func (s SKUCreation) Create(ctx context.Context, actor uint, in commerce.NormalizedSKU) (commerce.SKU, error) {
	var result commerce.SKU
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := administrator(tx, actor)
		if err != nil {
			return err
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&plan, in.SKU.PlanID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commerce.ErrNotFound
			}
			return err
		}
		sku := model.PlanSKU(in.SKU)
		if err := tx.Create(&sku).Error; err != nil {
			return skuWriteError(err)
		}
		// GORM's true default must not silently activate an explicitly inactive SKU.
		if !in.SKU.IsActive {
			if err := tx.Model(&sku).Update("is_active", false).Error; err != nil {
				return err
			}
			sku.IsActive = false
		}
		for _, operation := range in.AllowedOperations {
			if err := tx.Create(&model.PlanSKUOperation{PlanSKUID: sku.ID, Operation: operation}).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "plan.sku.create", Target: fmt.Sprintf("plan_sku:%d", sku.ID), Detail: fmt.Sprintf("plan=%d code=%s operations=%s", sku.PlanID, sku.Code, strings.Join(in.AllowedOperations, ","))}).Error; err != nil {
			return err
		}
		result = commerce.SKU(sku)
		return nil
	})
	if err != nil {
		return commerce.SKU{}, err
	}
	return result, nil
}
