package commerce

import "context"

type SKUUpdateRepository interface {
	Update(context.Context, uint, uint, NormalizedSKU) (SKU, error)
}
type SKUUpdate struct{ Repository SKUUpdateRepository }

func (s SKUUpdate) Update(ctx context.Context, actor, id uint, request SKURequest) (SKUView, error) {
	if actor == 0 {
		return SKUView{}, ErrPermission
	}
	if id == 0 {
		return SKUView{}, ErrNotFound
	}
	// The repository supplies the immutable parent identity from the locked SKU.
	normalized, err := NormalizeSKU(0, request)
	if err != nil {
		return SKUView{}, err
	}
	sku, err := s.Repository.Update(ctx, actor, id, normalized)
	if err != nil {
		return SKUView{}, err
	}
	return SKUView{SKU: sku, AllowedOperations: normalized.AllowedOperations, GrantTrafficBytes: sku.TrafficBytes}, nil
}
func ValidateSKUAvailability(published bool, otherPurchasable int, candidate NormalizedSKU) error {
	if !published || (candidate.SKU.IsActive && ContainsOperation(candidate.AllowedOperations, "purchase")) || otherPurchasable > 0 {
		return nil
	}
	return validationError("销售规格校验失败。", map[string]string{"allowed_operations": "已发布商品必须保留至少一个允许新购的可售 SKU。"})
}
func ValidatePublication(published bool, purchasable int) error {
	if !published || purchasable > 0 {
		return nil
	}
	return validationError("商品信息校验失败。", map[string]string{"is_active": "已发布商品至少需要一个允许新购的可售 SKU。"})
}
