package commerce

import (
	"context"
	"errors"
)

var ErrPermission = errors.New("commerce requires current administrator")
var ErrPlanNameConflict = errors.New("plan name already exists")
var ErrPlanSlugConflict = errors.New("plan slug already exists")
var ErrSKUCodeConflict = errors.New("SKU code already exists")
var ErrNotFound = errors.New("commerce resource not found")

// SKUCreationRepository commits a validated SKU, operation set and audit as one
// transaction, rechecking current administrator authority before writing.
type SKUCreationRepository interface {
	Create(context.Context, uint, NormalizedSKU) (SKU, error)
}
type SKUCreation struct{ Repository SKUCreationRepository }
type SKUView struct {
	SKU
	AllowedOperations []string `json:"allowed_operations"`
	GrantTrafficBytes int64    `json:"grant_traffic_bytes"`
}

func (s SKUCreation) Create(ctx context.Context, actor, planID uint, request SKURequest) (SKUView, error) {
	if actor == 0 {
		return SKUView{}, ErrPermission
	}
	if planID == 0 {
		return SKUView{}, ErrNotFound
	}
	normalized, err := NormalizeSKU(planID, request)
	if err != nil {
		return SKUView{}, err
	}
	sku, err := s.Repository.Create(ctx, actor, normalized)
	if err != nil {
		return SKUView{}, err
	}
	return SKUView{SKU: sku, AllowedOperations: normalized.AllowedOperations, GrantTrafficBytes: sku.TrafficBytes}, nil
}
