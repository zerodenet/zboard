package commercestore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type SKUQueries struct{ DB *gorm.DB }

func (s SKUQueries) Public(ctx context.Context, q commerce.SKUQuery) (commerce.SKURecords, error) {
	return s.list(ctx, 0, q, true)
}
func (s SKUQueries) Administrative(ctx context.Context, actor uint, q commerce.SKUQuery) (commerce.SKURecords, error) {
	return s.list(ctx, actor, q, false)
}
func (s SKUQueries) list(ctx context.Context, actor uint, q commerce.SKUQuery, public bool) (commerce.SKURecords, error) {
	out := commerce.SKURecords{Offset: q.Offset, Limit: q.Limit}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !public {
			if _, err := administrator(tx, actor); err != nil {
				return err
			}
		}
		var plan model.Plan
		parent := tx.Select("id").Where("id = ?", q.PlanID)
		if public {
			parent = parent.Where("is_active = ?", true)
		}
		if err := parent.First(&plan).Error; err != nil {
			return resourceError(err)
		}
		query := tx.Model(&model.PlanSKU{}).Where("plan_id = ?", q.PlanID)
		if public {
			query = query.Where("is_active = ?", true)
		} else if q.Active != nil {
			query = query.Where("is_active = ?", *q.Active)
		}
		if q.Search != "" {
			pattern := "%" + q.Search + "%"
			query = query.Where("LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(currency) LIKE ?", pattern, pattern, pattern)
		}
		if q.Operation != "" {
			query = query.Where("EXISTS (SELECT 1 FROM plan_sku_operations WHERE plan_sku_operations.plan_sku_id = plan_skus.id AND plan_sku_operations.operation = ?)", q.Operation)
		}
		if public && q.AnchorID > 0 {
			var anchor model.PlanSKU
			if err := query.Session(&gorm.Session{}).Where("id = ?", q.AnchorID).First(&anchor).Error; err != nil {
				return resourceError(err)
			}
			var preceding int64
			if err := query.Session(&gorm.Session{}).Where("sort_order < ? OR (sort_order = ? AND id < ?)", anchor.SortOrder, anchor.SortOrder, anchor.ID).Count(&preceding).Error; err != nil {
				return err
			}
			out.Offset = int(preceding) / q.Limit * q.Limit
		}
		if err := query.Session(&gorm.Session{}).Count(&out.Total).Error; err != nil {
			return err
		}
		var rows []model.PlanSKU
		if err := query.Order("sort_order asc, id asc").Offset(out.Offset).Limit(q.Limit).Find(&rows).Error; err != nil {
			return err
		}
		records, err := skuRecords(tx, rows)
		if err != nil {
			return err
		}
		out.Items = records
		return nil
	})
	if err != nil {
		return commerce.SKURecords{}, err
	}
	return out, nil
}
func (s SKUQueries) Get(ctx context.Context, actor, id uint) (commerce.SKURecord, error) {
	var out commerce.SKURecord
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := administrator(tx, actor); err != nil {
			return err
		}
		var sku model.PlanSKU
		if err := tx.First(&sku, id).Error; err != nil {
			return resourceError(err)
		}
		records, err := skuRecords(tx, []model.PlanSKU{sku})
		if err != nil {
			return err
		}
		out = records[0]
		return nil
	})
	if err != nil {
		return commerce.SKURecord{}, err
	}
	return out, nil
}
func skuRecords(tx *gorm.DB, rows []model.PlanSKU) ([]commerce.SKURecord, error) {
	out := make([]commerce.SKURecord, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var operations []model.PlanSKUOperation
	if err := tx.Where("plan_sku_id IN ?", ids).Order("plan_sku_id asc, operation asc").Find(&operations).Error; err != nil {
		return nil, err
	}
	byID := map[uint][]string{}
	for _, operation := range operations {
		byID[operation.PlanSKUID] = append(byID[operation.PlanSKUID], operation.Operation)
	}
	for _, row := range rows {
		out = append(out, commerce.SKURecord{SKU: commerce.SKU(row), Operations: byID[row.ID]})
	}
	return out, nil
}
