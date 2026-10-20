package commercestore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type checkoutDiscount struct {
	Context    commerce.DiscountQuoteContext
	Quote      commerce.DiscountQuote
	AcceptedAt time.Time
}

func discountCheckoutError(message string) error {
	return &commerce.ValidationError{Message: message, Fields: map[string]string{"discount": message}}
}

func withoutDiscount(request commerce.OrderCreateRequest) commerce.OrderCreateRequest {
	request.Discount = nil
	request.QuoteFingerprint = ""
	return request
}

func checkoutDiscountContext(order model.Order, selection string) (commerce.DiscountQuoteContext, error) {
	var policy commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil {
		return commerce.DiscountQuoteContext{}, invalidPurchaseSnapshot()
	}
	in, err := commerce.NewPurchaseDiscountContext(commerce.Order(order), policy, selection)
	if err != nil {
		return in, discountCheckoutError("本次优惠仅支持新购的首次优惠报价，请重新核对购买操作。")
	}
	return in, nil
}

// No database transaction or financial lock survives across the provider call.
func (s OrderCreation) evaluateDiscount(ctx context.Context, buyer uint, request commerce.OrderCreateRequest, create bool) (*model.Order, checkoutDiscount, error) {
	var in commerce.DiscountQuoteContext
	var existing *model.Order
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if create {
			found, _, _, err := existingOwnedCreation(tx, buyer, request)
			if err != nil {
				return err
			}
			if found != nil {
				existing = found
				return nil
			}
		}
		if request.Discount == nil || strings.TrimSpace(request.Discount.ProviderID) == "" || len(request.Discount.ProviderID) > 160 || len(request.Discount.Selection) > 256 {
			return discountCheckoutError("请选择一个有效的优惠提供方及规则。")
		}
		order, _, err := prepareOrder(tx, buyer, withoutDiscount(request), commerceNow(s.Now))
		if err != nil {
			return err
		}
		in, err = checkoutDiscountContext(order, request.Discount.Selection)
		return err
	})
	if err != nil || existing != nil {
		return existing, checkoutDiscount{}, err
	}
	if s.Discounts == nil {
		return nil, checkoutDiscount{}, commerce.ErrDiscountQuoteUnavailable
	}
	quote, err := s.Discounts.Quote(ctx, request.Discount.ProviderID, in)
	if err != nil {
		return nil, checkoutDiscount{}, err
	}
	if err := validateCheckoutOffer(in, quote, request.Discount.ProviderID, commerceNow(s.Now)); err != nil {
		return nil, checkoutDiscount{}, err
	}
	return nil, checkoutDiscount{Context: in, Quote: quote}, nil
}

func validateCheckoutOffer(in commerce.DiscountQuoteContext, quote commerce.DiscountQuote, provider string, now time.Time) error {
	if quote.Provider.PluginID != provider || commerce.ValidateDiscountQuote(in, quote, now) != nil {
		return commerce.ErrDiscountQuoteInvalid
	}
	if quote.Offer.Status != "available" {
		return discountCheckoutError("优惠当前不可直接接受，请重新核对优惠条款或稍后重试。")
	}
	if in.Operation != "new" || quote.Offer.Agreement == nil || quote.Offer.Agreement.Scope != "initial_only" {
		return discountCheckoutError("本次购买仅接入新购的首次优惠约定；长期优惠须另行确认。")
	}
	return nil
}

func applyCheckoutDiscount(order *model.Order, request commerce.OrderCreateRequest, accepted checkoutDiscount, now time.Time) error {
	if request.Discount == nil {
		return commerce.ErrDiscountQuoteInvalid
	}
	in, err := checkoutDiscountContext(*order, request.Discount.Selection)
	if err != nil {
		return err
	}
	before, _ := json.Marshal(accepted.Context)
	current, _ := json.Marshal(in)
	if string(before) != string(current) {
		return discountCheckoutError("商品价格、权益或购买条件已变化，请重新确认优惠报价。")
	}
	if err := validateCheckoutOffer(in, accepted.Quote, request.Discount.ProviderID, now); err != nil {
		return err
	}
	order.DiscountAmount = accepted.Quote.Offer.DiscountMinor
	order.PayableAmount = order.AmountCents - order.DiscountAmount
	return nil
}

func (s OrderCreation) createDiscounted(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.Order, error) {
	existing, accepted, err := s.evaluateDiscount(ctx, buyer, request, true)
	if err != nil {
		return commerce.Order{}, err
	}
	if existing != nil {
		return commerce.Order(*existing), nil
	}
	var out model.Order
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		accepted.AcceptedAt = commerceNow(s.Now)
		var err error
		out, err = createOwnedOrderWithDiscount(tx, buyer, request, &accepted, s.Now)
		return err
	})
	if err != nil {
		return commerce.Order{}, err
	}
	return commerce.Order(out), nil
}

func (s OrderCreation) previewDiscounted(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.OrderPreview, error) {
	_, accepted, err := s.evaluateDiscount(ctx, buyer, request, false)
	if err != nil {
		return commerce.OrderPreview{}, err
	}
	var out commerce.OrderPreview
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		accepted.AcceptedAt = commerceNow(s.Now)
		var err error
		var order model.Order
		order, out, err = prepareOrderWithDiscount(tx, buyer, request, accepted.AcceptedAt, &accepted)
		if err != nil {
			return err
		}
		return refreshDiscountAcceptance(&order, accepted, commerceNow(s.Now))
	})
	return out, err
}

func knownInitialPurchaseDiscount(order model.Order) bool {
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil || commerce.ValidatePurchaseDiscount(commerce.Order(order), policy) != nil {
		return false
	}
	// Reuse fulfillment's policy and presence checks as well as the monetary
	// checks above. Historical rows are never repaired from current catalog data.
	_, err = applyPurchaseSnapshot(order, entitlements.GrantPolicy{})
	return err == nil
}

// Recheck the trusted clock after financial locks and immediately before insert.
// Lock waits cannot turn an expired provider response into a new agreement.
func refreshDiscountAcceptance(order *model.Order, accepted checkoutDiscount, now time.Time) error {
	if err := validateCheckoutOffer(accepted.Context, accepted.Quote, accepted.Quote.Provider.PluginID, now); err != nil {
		return err
	}
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil || policy.Discount == nil {
		return invalidPurchaseSnapshot()
	}
	policy.Discount.AcceptedAt = now
	if commerce.ValidatePurchaseDiscount(commerce.Order(*order), policy) != nil {
		return invalidPurchaseSnapshot()
	}
	raw, err := json.Marshal(policy)
	if err != nil || len(raw) > commerce.MaxPurchaseSnapshotBytes {
		return invalidPurchaseSnapshot()
	}
	order.PurchaseSnapshot = string(raw)
	return nil
}
