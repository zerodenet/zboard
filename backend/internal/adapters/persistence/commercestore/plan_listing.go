package commercestore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type PlanListing struct{ DB *gorm.DB }

func (s PlanListing) Public(ctx context.Context, q commerce.PlanListQuery) (commerce.PlanListRecords, error) {
	return s.list(ctx, 0, q, true)
}
func (s PlanListing) Administrative(ctx context.Context, actor uint, q commerce.PlanListQuery) (commerce.PlanListRecords, error) {
	return s.list(ctx, actor, q, false)
}
func (s PlanListing) list(ctx context.Context, actor uint, q commerce.PlanListQuery, public bool) (commerce.PlanListRecords, error) {
	out := commerce.PlanListRecords{Items: []commerce.PlanDetailRecord{}, Legacy: []commerce.LegacyPlan{}}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !public {
			if _, err := administrator(tx, actor); err != nil {
				return err
			}
		}
		query := tx.Model(&model.Plan{})
		if q.PlanID > 0 {
			query = query.Where("plans.id = ?", q.PlanID)
		}
		if q.ExcludePlanID > 0 {
			query = query.Where("plans.id <> ?", q.ExcludePlanID)
		}
		if public || !q.IncludeInactive || q.Operation != "" {
			query = query.Where("plans.is_active = ?", true)
		}
		if !public && q.Active != nil {
			query = query.Where("plans.is_active = ?", *q.Active)
		}
		if q.Operation != "" {
			query = query.Where("EXISTS (SELECT 1 FROM plan_skus JOIN plan_sku_operations ON plan_sku_operations.plan_sku_id = plan_skus.id WHERE plan_skus.plan_id = plans.id AND plan_skus.is_active = ? AND plan_sku_operations.operation = ?)", true, q.Operation)
		}
		if q.Search != "" {
			pattern := "%" + q.Search + "%"
			query = query.Where("LOWER(plans.name) LIKE ? OR LOWER(plans.slug) LIKE ? OR LOWER(plans.summary) LIKE ?", pattern, pattern, pattern)
		}
		if err := query.Session(&gorm.Session{}).Count(&out.Total).Error; err != nil {
			return err
		}
		query = query.Order("sort_order asc, id desc").Preload("NodeGroup")
		operation := q.Operation
		if operation == "" {
			operation = "purchase"
		}
		if q.LegacyArray {
			query = query.Preload("SKUs", func(db *gorm.DB) *gorm.DB {
				if public || q.Operation != "" {
					db = db.Where("is_active = ?", true).Where("EXISTS (SELECT 1 FROM plan_sku_operations WHERE plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?)", operation)
				}
				return db.Order("sort_order asc, id asc")
			})
		} else {
			query = query.Offset(q.Offset).Limit(q.Limit)
		}
		var plans []model.Plan
		if err := query.Find(&plans).Error; err != nil {
			return err
		}
		if q.LegacyArray {
			for _, plan := range plans {
				item := commerce.LegacyPlan{Plan: planView(plan)}
				if plan.NodeGroup != nil {
					group := commerce.LegacyPlanGroup(*plan.NodeGroup)
					item.NodeGroup = &group
				}
				out.Legacy = append(out.Legacy, item)
			}
			return nil
		}
		ids := make([]uint, 0, len(plans))
		for _, plan := range plans {
			ids = append(ids, plan.ID)
		}
		var counts map[uint]commerce.PlanSKUCounts
		var err error
		if public || q.Operation != "" {
			counts, err = planCountsForOperation(tx, ids, operation)
		} else {
			counts, err = allPlanSKUCounts(tx, ids)
		}
		if err != nil {
			return err
		}
		primary, err := primaryPlanSKUs(tx, ids, operation)
		if err != nil {
			return err
		}
		for _, plan := range plans {
			record := commerce.PlanDetailRecord{Plan: planView(plan), Counts: counts[plan.ID]}
			if plan.NodeGroup != nil {
				record.Group = &commerce.PlanGroupSummary{ID: plan.NodeGroup.ID, Name: plan.NodeGroup.Name, Code: plan.NodeGroup.Code, IsEnabled: plan.NodeGroup.IsEnabled}
			}
			if sku, ok := primary[plan.ID]; ok {
				value := commerce.SKU(sku)
				record.Primary = &value
			}
			out.Items = append(out.Items, record)
		}
		return nil
	})
	if err != nil {
		return commerce.PlanListRecords{}, err
	}
	return out, nil
}
