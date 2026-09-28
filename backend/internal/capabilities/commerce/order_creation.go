package commerce

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
)

var ErrBuyerUnavailable = errors.New("order requires current active buyer")
var ErrSubscriptionCapacity = entitlements.ErrCapacity

type OrderCreationRepository interface {
	Create(context.Context, uint, OrderCreateRequest) (Order, error)
	Preview(context.Context, uint, OrderCreateRequest) (OrderPreview, error)
}
type OrderCreation struct{ Repository OrderCreationRepository }

// Create buys for the authenticated actor. The request cannot select another buyer.
func (s OrderCreation) Create(ctx context.Context, buyer uint, request OrderCreateRequest) (Order, error) {
	if buyer == 0 {
		return Order{}, ErrBuyerUnavailable
	}
	if request.PlanSKUID == 0 {
		return Order{}, validationError("订单创建失败。", map[string]string{"plan_sku_id": "请选择销售规格。"})
	}
	return s.Repository.Create(ctx, buyer, request)
}

func (s OrderCreation) Preview(ctx context.Context, buyer uint, request OrderCreateRequest) (OrderPreview, error) {
	if buyer == 0 {
		return OrderPreview{}, ErrBuyerUnavailable
	}
	if request.PlanSKUID == 0 {
		return OrderPreview{}, validationError("请选择销售规格。", nil)
	}
	return s.Repository.Preview(ctx, buyer, request)
}
