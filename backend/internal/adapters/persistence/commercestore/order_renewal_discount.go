package commercestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type renewalPriceResolution struct {
	Source    model.Order
	Purchased bool
}

func resolveOrderRenewalPrice(tx *gorm.DB, order *model.Order, sub model.Subscription, now time.Time) (*renewalPriceResolution, error) {
	grace, err := entitlementstore.RecoveryGracePeriod(tx)
	if err != nil {
		return nil, err
	}
	if !entitlements.CanRenewAt(entitlements.Subscription(sub), now, grace) {
		return nil, quoteError(entitlements.ErrRenewalWindow.Error())
	}
	source, purchased, err := renewalPriceAgreement(tx, *order, sub.ID)
	if err != nil {
		return nil, err
	}
	order.AmountCents, order.PayableAmount = source.AmountCents, source.AmountCents
	return &renewalPriceResolution{source, purchased}, nil
}
func quoteResolvedRenewal(order model.Order, sub model.Subscription, resolved renewalPriceResolution) (commerce.OrderPreview, error) {
	preview := commerce.OrderPreview{OrderType: order.OrderType, AmountCents: order.AmountCents, PayableAmount: order.PayableAmount, Currency: order.Currency, TrafficBytes: order.TrafficBytes, PriceBasis: "purchased_period", EndAt: &sub.EndAt}
	if !resolved.Purchased {
		preview.PriceBasis = "current_catalog"
		preview.RequiresPriceAcceptance = true
	}
	token, err := json.Marshal(struct {
		Source, Target, SKU uint
		Base                int64
		Currency, Unit      string
		Period              int
		EndAt               time.Time
		Purchased           bool
	}{resolved.Source.ID, sub.ID, order.PlanSKUID, resolved.Source.AmountCents, order.Currency, order.BillingUnit, order.BillingValue, sub.EndAt, resolved.Purchased})
	if err != nil {
		return preview, err
	}
	sum := sha256.Sum256(token)
	preview.QuoteFingerprint = hex.EncodeToString(sum[:])
	return preview, nil
}

func (s OrderCreation) existingTargetCreation(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (*model.Order, error) {
	var out *model.Order
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, _, _, err = existingOwnedCreation(tx, buyer, request)
		return err
	})
	return out, err
}

func (s OrderCreation) evaluateRenewal(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (*checkoutDiscount, error) {
	// Only this preflight uses ordinary reads before source locks; it is outside
	// both financial transactions. No RR snapshot leaks into either transaction.
	scope, err := prepareDiscountSourceScope(s.DB.WithContext(ctx), buyer, request)
	if err != nil {
		return nil, err
	}
	marker := &checkoutDiscount{Baseline: true, Scope: scope}
	var order model.Order
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPreparedDiscountSources(tx, scope); err != nil {
			return err
		}
		var err error
		order, _, err = prepareOrderWithDiscount(tx, buyer, withoutDiscount(request), commerceNow(s.Now), marker)
		return err
	})
	if err != nil {
		return nil, err
	}
	if order.OrderType != "renewal" {
		if request.Discount == nil {
			return nil, nil
		}
		return s.evaluateOperationDiscount(ctx, order, request)
	}
	if marker.Source == nil {
		if request.Discount != nil {
			return nil, discountCheckoutError("该续费尚无可继承的已付款长期优惠协议，不能从页面选择重建。")
		}
		return nil, nil
	}
	source := marker.Source
	if request.Discount != nil && (request.Discount.ProviderID != source.ProviderID || request.Discount.Selection != source.Selection) {
		return nil, discountCheckoutError("续费必须使用已购协议的提供方与规则，不能由页面替换。")
	}
	var policy commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil {
		return nil, invalidPurchaseSnapshot()
	}
	in, err := commerce.NewRecurringDiscountContext(commerce.Order(order), policy, source.Selection, source)
	if err != nil {
		return nil, err
	}
	if s.Discounts == nil {
		return nil, commerce.ErrDiscountQuoteUnavailable
	}
	quote, err := s.Discounts.Quote(ctx, source.ProviderID, in)
	if err != nil {
		return nil, err
	}
	if err := validateCheckoutOffer(in, quote, source.ProviderID, commerceNow(s.Now)); err != nil {
		return nil, err
	}
	if quote.Provider.SigningKeyFingerprint != source.SigningKeyFingerprint {
		return nil, commerce.ErrDiscountQuoteInvalid
	}
	return &checkoutDiscount{Version: 2, Source: source, Context: in, Quote: quote}, nil
}

func (s OrderCreation) createRenewalAwareOrder(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.Order, error) {
	existing, err := s.existingTargetCreation(ctx, buyer, request)
	if err != nil {
		return commerce.Order{}, err
	}
	if existing != nil {
		return commerce.Order(*existing), nil
	}
	accepted, err := s.evaluateRenewal(ctx, buyer, request)
	if err != nil {
		return commerce.Order{}, err
	}
	scope, err := prepareDiscountSourceScope(s.DB.WithContext(ctx), buyer, request)
	if err != nil {
		return commerce.Order{}, err
	}
	if accepted == nil {
		accepted = &checkoutDiscount{GuardOnly: true}
	}
	accepted.Scope = scope
	var out model.Order
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPreparedDiscountSources(tx, scope); err != nil {
			return err
		}
		accepted.AcceptedAt = commerceNow(s.Now)
		var err error
		out, err = createOwnedOrderWithDiscount(tx, buyer, request, accepted, s.Now)
		return err
	})
	if err != nil {
		return commerce.Order{}, err
	}
	return commerce.Order(out), nil
}

func (s OrderCreation) previewRenewalAwareOrder(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.OrderPreview, error) {
	accepted, err := s.evaluateRenewal(ctx, buyer, request)
	if err != nil {
		return commerce.OrderPreview{}, err
	}
	scope, err := prepareDiscountSourceScope(s.DB.WithContext(ctx), buyer, request)
	if err != nil {
		return commerce.OrderPreview{}, err
	}
	if accepted == nil {
		accepted = &checkoutDiscount{GuardOnly: true}
	}
	accepted.Scope = scope
	var out commerce.OrderPreview
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPreparedDiscountSources(tx, scope); err != nil {
			return err
		}
		accepted.AcceptedAt = commerceNow(s.Now)
		order, preview, err := prepareOrderWithDiscount(tx, buyer, request, accepted.AcceptedAt, accepted)
		if err != nil {
			return err
		}
		out = preview
		if hasCheckoutOffer(accepted) {
			return refreshDiscountAcceptance(&order, *accepted, commerceNow(s.Now))
		}
		return nil
	})
	if err != nil {
		return commerce.OrderPreview{}, err
	}
	return out, nil
}
