package commercestore

import (
	"context"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
)

// Called only after preflight's transaction has ended. It owns no SQL/source,
// wallet or entitlement lock across the native provider RPC.
func (s OrderCreation) evaluateOperationDiscount(ctx context.Context, order model.Order, request commerce.OrderCreateRequest) (*checkoutDiscount, error) {
	if request.Discount == nil || strings.TrimSpace(request.Discount.ProviderID) == "" || len(request.Discount.ProviderID) > 160 || len(request.Discount.Selection) > 256 {
		return nil, discountCheckoutError("请选择有效的本单优惠提供方与规则。")
	}
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil {
		return nil, invalidPurchaseSnapshot()
	}
	in, err := commerce.NewOperationDiscountContext(commerce.Order(order), policy, request.Discount.Selection)
	if err != nil {
		return nil, err
	}
	if s.Discounts == nil {
		return nil, commerce.ErrDiscountQuoteUnavailable
	}
	quote, err := s.Discounts.Quote(ctx, request.Discount.ProviderID, in)
	if err != nil {
		return nil, err
	}
	if err := validateCheckoutOffer(in, quote, request.Discount.ProviderID, commerceNow(s.Now)); err != nil {
		return nil, err
	}
	return &checkoutDiscount{Version: 3, Context: in, Quote: quote}, nil
}
