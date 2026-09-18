package commercestore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type PlanDetails struct{ DB *gorm.DB }

func (s PlanDetails) Public(ctx context.Context, id uint) (commerce.PlanDetailRecord, error) {
	return s.read(ctx, 0, id, true)
}
func (s PlanDetails) Administrative(ctx context.Context, actor, id uint) (commerce.PlanDetailRecord, error) {
	return s.read(ctx, actor, id, false)
}
func (s PlanDetails) read(ctx context.Context, actor, id uint, public bool) (commerce.PlanDetailRecord, error) {
	var out commerce.PlanDetailRecord
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !public {
			if _, err := administrator(tx, actor); err != nil {
				return err
			}
		}
		query := tx.Where("id = ?", id)
		if public {
			query = query.Where("is_active = ?", true)
		}
		var plan model.Plan
		if err := query.First(&plan).Error; err != nil {
			return resourceError(err)
		}
		out.Plan = planView(plan)
		// Related group is a read-only projection; absence is compatible with a
		// historical orphan and must not turn a database failure into missing data.
		var group commerce.PlanGroupSummary
		found := tx.Model(&model.NodeGroup{}).Select("id", "name", "code", "is_enabled").Where("id = ?", plan.NodeGroupID).Limit(1).Find(&group)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			out.Group = &group
		}
		counts := tx.Model(&model.PlanSKU{}).Where("plan_id = ?", id)
		if public {
			counts = counts.Where("is_active = ?", true).Where("EXISTS (SELECT 1 FROM plan_sku_operations WHERE plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?)", "purchase")
		}
		if err := counts.Select("COUNT(*) AS sku_count, COALESCE(SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END), 0) AS active_sku_count").Scan(&out.Counts).Error; err != nil {
			return err
		}
		out.Counts.PlanID = id
		if public {
			var sku model.PlanSKU
			result := tx.Where("plan_id = ? AND is_active = ?", id, true).
				Where("EXISTS (SELECT 1 FROM plan_sku_operations WHERE plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?)", "purchase").
				Order("price_cents asc, sort_order asc, id asc").First(&sku)
			if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			if result.Error == nil {
				value := commerce.SKU(sku)
				out.Primary = &value
			}
		}
		return nil
	})
	if err != nil {
		return commerce.PlanDetailRecord{}, err
	}
	return out, nil
}
