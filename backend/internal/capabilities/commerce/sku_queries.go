package commerce

import (
	"context"
	"sort"
	"strings"
)

type SKUQuery struct {
	PlanID        uint
	Search        string
	Active        *bool
	Operation     string
	LegacyType    string
	AnchorID      uint
	Offset, Limit int
}
type SKURecord struct {
	SKU        SKU
	Operations []string
}
type SKURecords struct {
	Items         []SKURecord
	Total         int64
	Offset, Limit int
}
type SKUPage struct {
	Items         []SKUView
	Total         int64
	Offset, Limit int
}
type SKUQueryRepository interface {
	Public(context.Context, SKUQuery) (SKURecords, error)
	Administrative(context.Context, uint, SKUQuery) (SKURecords, error)
	Get(context.Context, uint, uint) (SKURecord, error)
}
type SKUQueries struct{ Repository SKUQueryRepository }

func (s SKUQueries) Public(ctx context.Context, query SKUQuery) (SKUPage, error) {
	query, err := NormalizeSKUQuery(query, true)
	if err != nil {
		return SKUPage{}, err
	}
	records, err := s.Repository.Public(ctx, query)
	if err != nil {
		return SKUPage{}, err
	}
	return skuPage(records), nil
}
func (s SKUQueries) Administrative(ctx context.Context, actor uint, query SKUQuery) (SKUPage, error) {
	if actor == 0 {
		return SKUPage{}, ErrPermission
	}
	query, err := NormalizeSKUQuery(query, false)
	if err != nil {
		return SKUPage{}, err
	}
	records, err := s.Repository.Administrative(ctx, actor, query)
	if err != nil {
		return SKUPage{}, err
	}
	return skuPage(records), nil
}
func (s SKUQueries) Get(ctx context.Context, actor, id uint) (SKUView, error) {
	if actor == 0 {
		return SKUView{}, ErrPermission
	}
	if id == 0 {
		return SKUView{}, ErrNotFound
	}
	record, err := s.Repository.Get(ctx, actor, id)
	if err != nil {
		return SKUView{}, err
	}
	return ProjectSKU(record), nil
}
func NormalizeSKUQuery(q SKUQuery, public bool) (SKUQuery, error) {
	if q.PlanID == 0 {
		return q, ErrNotFound
	}
	if q.Offset < 0 {
		return q, validationError("offset must be a non-negative integer", nil)
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 {
		return q, validationError("limit must be an integer between 1 and 200", nil)
	}
	q.Search = strings.ToLower(strings.TrimSpace(q.Search))
	if len(q.Search) > 128 {
		return q, validationError("q must not exceed 128 bytes", nil)
	}
	q.Operation = strings.TrimSpace(q.Operation)
	if public && q.Operation == "" {
		legacy := strings.TrimSpace(q.LegacyType)
		if legacy != "" {
			switch legacy {
			case "new", "renewal", "upgrade", "traffic_pack":
				q.Operation = LegacySKUTypeOperation(legacy)
			default:
				return q, validationError("invalid sku_type", nil)
			}
		}
	}
	q.Operation = strings.ToLower(q.Operation)
	if q.Operation != "" {
		if _, valid := OperationRank(q.Operation); !valid {
			return q, validationError("invalid operation", nil)
		}
	}
	if public {
		q.Active = nil
	} else {
		q.AnchorID = 0
	}
	return q, nil
}
func skuPage(records SKURecords) SKUPage {
	page := SKUPage{Items: make([]SKUView, 0, len(records.Items)), Total: records.Total, Offset: records.Offset, Limit: records.Limit}
	for _, record := range records.Items {
		page.Items = append(page.Items, ProjectSKU(record))
	}
	return page
}
func ProjectSKU(record SKURecord) SKUView {
	sku := record.SKU
	operations := append([]string(nil), record.Operations...)
	if sku.BillingMode == "" {
		if sku.BillingUnit == "once" || sku.SKUType == "traffic_pack" {
			sku.BillingMode = "one_time"
		} else {
			sku.BillingMode = "periodic"
		}
	}
	if sku.EntitlementMode == "" {
		if sku.SKUType == "traffic_pack" {
			sku.EntitlementMode = "traffic_addon"
		} else {
			sku.EntitlementMode = "plan"
		}
	}
	if len(operations) == 0 {
		operations = []string{LegacySKUTypeOperation(sku.SKUType)}
	}
	sort.SliceStable(operations, func(i, j int) bool {
		left, _ := OperationRank(operations[i])
		right, _ := OperationRank(operations[j])
		return left < right
	})
	if sku.RenewalEffect == "" {
		sku.RenewalEffect = DefaultRenewalEffect(sku.BillingUnit, sku.EntitlementMode, operations)
	}
	grant := int64(0)
	if sku.EntitlementMode == "traffic_addon" {
		grant = sku.TrafficBytes
	}
	return SKUView{SKU: sku, AllowedOperations: operations, GrantTrafficBytes: grant}
}
