package commerce

import "context"

type PlanDetailRecord struct {
	Plan    Plan
	Group   *PlanGroupSummary
	Counts  PlanSKUCounts
	Primary *SKU
}
type PlanDetailRepository interface {
	Public(context.Context, uint) (PlanDetailRecord, error)
	Administrative(context.Context, uint, uint) (PlanDetailRecord, error)
}
type PlanDetails struct{ Repository PlanDetailRepository }

func (s PlanDetails) Public(ctx context.Context, id uint) (PlanCatalog, error) {
	if id == 0 {
		return PlanCatalog{}, ErrNotFound
	}
	record, err := s.Repository.Public(ctx, id)
	if err != nil {
		return PlanCatalog{}, err
	}
	if !record.Plan.IsActive {
		return PlanCatalog{}, ErrNotFound
	}
	return CatalogPlan(record.Plan, record.Group, record.Counts, record.Primary), nil
}
func (s PlanDetails) Administrative(ctx context.Context, actor, id uint) (PlanDetail, error) {
	if actor == 0 {
		return PlanDetail{}, ErrPermission
	}
	if id == 0 {
		return PlanDetail{}, ErrNotFound
	}
	record, err := s.Repository.Administrative(ctx, actor, id)
	if err != nil {
		return PlanDetail{}, err
	}
	return DetailPlan(record.Plan, record.Group, record.Counts), nil
}
