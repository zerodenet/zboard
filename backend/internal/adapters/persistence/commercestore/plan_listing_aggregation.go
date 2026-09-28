package commercestore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type planSKUCountRow struct {
	PlanID         uint
	SKUCount       int64
	ActiveSKUCount int64
}

func planCountsForOperation(db *gorm.DB, planIDs []uint, operation string) (map[uint]commerce.PlanSKUCounts, error) {
	counts := make(map[uint]commerce.PlanSKUCounts, len(planIDs))
	if len(planIDs) == 0 {
		return counts, nil
	}
	rows := make([]planSKUCountRow, 0, len(planIDs))
	if err := db.Table("plan_skus").
		Select("plan_skus.plan_id, COUNT(*) AS sku_count, COUNT(*) AS active_sku_count").
		Joins("JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?", operation).
		Where("plan_skus.plan_id IN ? AND plan_skus.is_active = ?", planIDs, true).
		Group("plan_skus.plan_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.PlanID] = commerce.PlanSKUCounts{PlanID: row.PlanID, SKUCount: row.SKUCount, ActiveSKUCount: row.ActiveSKUCount}
	}
	return counts, nil
}

func primaryPlanSKUs(db *gorm.DB, planIDs []uint, operation string) (map[uint]model.PlanSKU, error) {
	items := make(map[uint]model.PlanSKU, len(planIDs))
	if len(planIDs) == 0 {
		return items, nil
	}
	rows := make([]model.PlanSKU, 0, len(planIDs))
	if err := db.Table("plan_skus AS candidate").
		Joins("JOIN plan_sku_operations AS candidate_operation ON candidate_operation.plan_sku_id = candidate.id AND candidate_operation.operation = ?", operation).
		Where("candidate.plan_id IN ? AND candidate.is_active = ?", planIDs, true).
		Where(`NOT EXISTS (
			SELECT 1
			FROM plan_skus AS earlier
			JOIN plan_sku_operations AS earlier_operation
			  ON earlier_operation.plan_sku_id = earlier.id
			 AND earlier_operation.operation = ?
			WHERE earlier.plan_id = candidate.plan_id
			  AND earlier.is_active = 1
			  AND (
			    earlier.price_cents < candidate.price_cents
			    OR (earlier.price_cents = candidate.price_cents AND earlier.sort_order < candidate.sort_order)
			    OR (earlier.price_cents = candidate.price_cents AND earlier.sort_order = candidate.sort_order AND earlier.id < candidate.id)
			  )
		)`, operation).
		Order("candidate.plan_id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		items[row.PlanID] = row
	}
	return items, nil
}

func allPlanSKUCounts(db *gorm.DB, planIDs []uint) (map[uint]commerce.PlanSKUCounts, error) {
	counts := make(map[uint]commerce.PlanSKUCounts, len(planIDs))
	if len(planIDs) == 0 {
		return counts, nil
	}
	rows := make([]planSKUCountRow, 0, len(planIDs))
	if err := db.Model(&model.PlanSKU{}).
		Select("plan_id, COUNT(*) AS sku_count, SUM(CASE WHEN is_active = 1 THEN 1 ELSE 0 END) AS active_sku_count").
		Where("plan_id IN ? AND archived_at IS NULL", planIDs).
		Group("plan_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.PlanID] = commerce.PlanSKUCounts{PlanID: row.PlanID, SKUCount: row.SKUCount, ActiveSKUCount: row.ActiveSKUCount}
	}
	var sales []struct {
		PlanID uint
		commerce.PlanSalesOption
	}
	if err := db.Table("plan_skus AS sku").
		Select("sku.plan_id, operation.operation, sku.currency, MIN(sku.price_cents) AS min_price_cents, MAX(sku.price_cents) AS max_price_cents").
		Joins("JOIN plan_sku_operations AS operation ON operation.plan_sku_id = sku.id").
		Joins("JOIN plans AS plan ON plan.id = sku.plan_id").
		Where("sku.plan_id IN ? AND sku.archived_at IS NULL AND sku.is_active = ? AND plan.archived_at IS NULL AND plan.is_active = ?", planIDs, true, true).
		Where("operation.operation IN ? AND (operation.operation <> ? OR plan.is_renewable = ?)", []string{"purchase", "renew", "change", "addon", "reset"}, "renew", true).
		Group("sku.plan_id, operation.operation, sku.currency").Order("sku.plan_id, operation.operation, sku.currency").Scan(&sales).Error; err != nil {
		return nil, err
	}
	for _, row := range sales {
		value := counts[row.PlanID]
		value.SalesOptions = append(value.SalesOptions, row.PlanSalesOption)
		counts[row.PlanID] = value
	}
	return counts, nil
}
