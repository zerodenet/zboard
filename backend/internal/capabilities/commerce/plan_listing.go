package commerce

import (
	"context"
	"strings"
)

type PlanListQuery struct {
	PlanID, ExcludePlanID uint
	Operation, Search     string
	IncludeInactive       bool
	Active                *bool
	Offset, Limit         int
	// LegacyArray preserves the old HTTP array contract. New callers default to
	// bounded pages; legacy callers still require a later compatibility migration.
	LegacyArray bool
}
type PlanListRecords struct {
	Items  []PlanDetailRecord
	Legacy []LegacyPlan
	Total  int64
}
type PlanListResult struct {
	Items         []PlanCatalog
	Legacy        []LegacyPlan
	Total         int64
	Offset, Limit int
}
type PlanListingRepository interface {
	Public(context.Context, PlanListQuery) (PlanListRecords, error)
	Administrative(context.Context, uint, PlanListQuery) (PlanListRecords, error)
}
type PlanListing struct{ Repository PlanListingRepository }

func (s PlanListing) Public(ctx context.Context, q PlanListQuery) (PlanListResult, error) {
	q.IncludeInactive = false
	q.Active = nil
	return s.list(ctx, 0, q)
}
func (s PlanListing) Administrative(ctx context.Context, actor uint, q PlanListQuery) (PlanListResult, error) {
	if actor == 0 {
		return PlanListResult{}, ErrPermission
	}
	return s.list(ctx, actor, q)
}
func (s PlanListing) list(ctx context.Context, actor uint, q PlanListQuery) (PlanListResult, error) {
	q.Operation = strings.TrimSpace(q.Operation)
	if q.Operation != "" {
		if _, valid := OperationRank(q.Operation); !valid {
			return PlanListResult{}, validationError("invalid operation", nil)
		}
	}
	if q.LegacyArray {
		q.Search = ""
		q.Offset = 0
		q.Limit = 0
	} else {
		if q.Limit == 0 {
			q.Limit = 50
		}
		if q.Offset < 0 || q.Limit < 1 || q.Limit > 200 {
			return PlanListResult{}, validationError("invalid pagination", nil)
		}
		q.Search = strings.ToLower(strings.TrimSpace(q.Search))
		if len(q.Search) > 128 {
			return PlanListResult{}, validationError("q must not exceed 128 bytes", nil)
		}
	}
	var records PlanListRecords
	var err error
	if actor == 0 {
		records, err = s.Repository.Public(ctx, q)
	} else {
		records, err = s.Repository.Administrative(ctx, actor, q)
	}
	if err != nil {
		return PlanListResult{}, err
	}
	out := PlanListResult{Items: make([]PlanCatalog, 0, len(records.Items)), Legacy: records.Legacy, Total: records.Total, Offset: q.Offset, Limit: q.Limit}
	for _, record := range records.Items {
		out.Items = append(out.Items, CatalogPlan(record.Plan, record.Group, record.Counts, record.Primary))
	}
	return out, nil
}
