package commercestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type PlanCreation struct{ DB *gorm.DB }

func (s PlanCreation) Create(ctx context.Context, actor uint, in commerce.NewPlan) (commerce.Plan, error) {
	var out commerce.Plan
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := administrator(tx, actor)
		if err != nil {
			return err
		}
		// This read preserves aggregate field feedback. Unique constraints remain
		// authoritative if a competing transaction inserts after this snapshot.
		if err := creationIdentifiers(tx, in); err != nil {
			return err
		}
		exists, enabled, hasEndpoint, err := networkstore.CommerceGroupAvailability(tx, in.Plan.NodeGroupID)
		if err != nil {
			return err
		}
		request := commerce.PlanUpdateRequest{NodeGroupID: &in.Plan.NodeGroupID, IsActive: &in.Plan.IsActive}
		if err := commerce.ValidatePlanGroup(in.Plan, request, exists, enabled, hasEndpoint); err != nil {
			return err
		}
		plan := planRow(in.Plan)
		if err := tx.Create(&plan).Error; err != nil {
			switch mapped := planWriteError(err); {
			case errors.Is(mapped, commerce.ErrPlanNameConflict):
				return &commerce.IdentifierConflict{Fields: map[string]string{"name": "该商品名称已被使用，请更换后重试。"}}
			case errors.Is(mapped, commerce.ErrPlanSlugConflict):
				return &commerce.IdentifierConflict{Fields: map[string]string{"slug": "该商品标识已被其他商品使用，请更换后重试。"}}
			default:
				return mapped
			}
		}

		// GORM defaults must not turn a draft or nonrenewable plan on.
		if err := tx.Model(&plan).Updates(map[string]interface{}{"is_active": in.Plan.IsActive, "is_renewable": in.Plan.IsRenewable}).Error; err != nil {
			return err
		}
		for i, input := range in.SKUs {
			sku := model.PlanSKU(input.SKU)
			sku.PlanID = plan.ID
			if err := tx.Create(&sku).Error; err != nil {
				if uniqueViolation(err) {
					return &commerce.IdentifierConflict{Fields: map[string]string{fmt.Sprintf("skus.%d.code", i): "该 SKU 编码已被其他销售规格使用，请更换后重试。"}}
				}
				return err
			}
			if !input.SKU.IsActive {
				if err := tx.Model(&sku).Update("is_active", false).Error; err != nil {
					return err
				}
			}
			for _, operation := range input.AllowedOperations {
				if err := tx.Create(&model.PlanSKUOperation{PlanSKUID: sku.ID, Operation: operation}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "plan.create", Target: fmt.Sprintf("plan:%d", plan.ID), Detail: fmt.Sprintf("skus=%d node_group=%d operation_model=v2", len(in.SKUs), plan.NodeGroupID)}).Error; err != nil {
			return err
		}
		if err := tx.Preload("SKUs", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order asc, id asc") }).First(&plan, plan.ID).Error; err != nil {
			return err
		}
		out = planView(plan)
		return nil
	})
	if err != nil {
		return commerce.Plan{}, err
	}
	return out, nil
}
func creationIdentifiers(tx *gorm.DB, in commerce.NewPlan) error {
	fields := map[string]string{}
	var count int64
	if err := tx.Model(&model.Plan{}).Where("slug = ?", in.Plan.Slug).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		fields["slug"] = "该商品标识已被其他商品使用，请更换后重试。"
	}
	codes := make([]string, 0, len(in.SKUs))
	for _, input := range in.SKUs {
		codes = append(codes, input.SKU.Code)
	}
	existing := map[string]bool{}
	for start := 0; start < len(codes); start += 200 {
		end := start + 200
		if end > len(codes) {
			end = len(codes)
		}
		var rows []struct{ Code string }
		if err := tx.Model(&model.PlanSKU{}).Select("code").Where("code IN ?", codes[start:end]).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			existing[row.Code] = true
		}
	}
	for i, input := range in.SKUs {
		if existing[input.SKU.Code] {
			fields[fmt.Sprintf("skus.%d.code", i)] = "该 SKU 编码已被其他销售规格使用，请更换后重试。"
		}
	}

	if len(fields) > 0 {
		return &commerce.IdentifierConflict{Fields: fields}
	}
	return nil
}
