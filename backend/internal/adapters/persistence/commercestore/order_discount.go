package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type checkoutDiscount struct {
	Version    int
	Source     *commerce.DiscountSourceSnapshot
	Scope      *discountSourceScope
	Baseline   bool
	GuardOnly  bool
	Context    commerce.DiscountQuoteContext
	Quote      commerce.DiscountQuote
	AcceptedAt time.Time
}

func hasCheckoutOffer(discount *checkoutDiscount) bool {
	return discount != nil && !discount.Baseline && !discount.GuardOnly
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
	version := 1
	if quote.Offer.Agreement.Scope == "recurring" {
		version = 2
	}
	return nil, checkoutDiscount{Version: version, Context: in, Quote: quote}, nil
}

func validateCheckoutOffer(in commerce.DiscountQuoteContext, quote commerce.DiscountQuote, provider string, now time.Time) error {
	if quote.Provider.PluginID != provider || commerce.ValidateDiscountQuote(in, quote, now) != nil {
		return commerce.ErrDiscountQuoteInvalid
	}
	if quote.Offer.Status != "available" {
		invalid := &commerce.ValidationError{Message: "优惠状态为" + quote.Offer.Status + "，当前不能接受；请先核对协议。", Fields: map[string]string{"discount_status": quote.Offer.Status}}
		if quote.Offer.Status == "requires_confirmation" {
			return errors.Join(commerce.ErrDiscountConfirmationRequired, invalid)
		}
		return invalid
	}
	if quote.Offer.Agreement == nil {
		return discountCheckoutError("当前购买操作缺少可核对的优惠约定。")
	}
	switch in.Operation {
	case "new":
		if quote.Offer.Agreement.Scope != "initial_only" && quote.Offer.Agreement.Scope != "recurring" {
			return commerce.ErrDiscountQuoteInvalid
		}
	case "renewal":
		if quote.Offer.Agreement.Scope != "recurring" {
			return commerce.ErrDiscountQuoteInvalid
		}
	case "upgrade", "traffic_reset", "traffic_pack":
		if quote.Offer.Agreement.Scope != commerce.OperationDiscountScope {
			return discountCheckoutError("本次操作仅支持明确的本单优惠，不继承或替换续费约定。")
		}
	default:
		return commerce.ErrDiscountQuoteInvalid
	}
	return nil
}

func applyCheckoutDiscount(order *model.Order, request commerce.OrderCreateRequest, accepted checkoutDiscount, now time.Time, sources ...*commerce.DiscountSourceSnapshot) error {
	var in commerce.DiscountQuoteContext
	var err error
	provider := accepted.Quote.Provider.PluginID
	selection := accepted.Context.Selection
	if accepted.Version == 3 {
		if request.Discount == nil {
			return commerce.ErrDiscountQuoteInvalid
		}
		provider = request.Discount.ProviderID
		var policy commerce.PurchaseSnapshot
		if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil {
			return invalidPurchaseSnapshot()
		}
		in, err = commerce.NewOperationDiscountContext(commerce.Order(*order), policy, request.Discount.Selection)
	} else if accepted.Version == 2 {
		var source *commerce.DiscountSourceSnapshot
		if len(sources) > 0 {
			source = sources[0]
		}
		var policy commerce.PurchaseSnapshot
		if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil {
			return invalidPurchaseSnapshot()
		}
		in, err = commerce.NewRecurringDiscountContext(commerce.Order(*order), policy, selection, source)
		if source != nil && (provider != source.ProviderID || accepted.Quote.Provider.SigningKeyFingerprint != source.SigningKeyFingerprint) {
			return commerce.ErrDiscountQuoteInvalid
		}
	} else {
		if request.Discount == nil {
			return commerce.ErrDiscountQuoteInvalid
		}
		provider = request.Discount.ProviderID
		in, err = checkoutDiscountContext(*order, request.Discount.Selection)
	}
	if err != nil {
		return err
	}
	before, _ := json.Marshal(accepted.Context)
	current, _ := json.Marshal(in)
	if string(before) != string(current) {
		return discountCheckoutError("商品、协议来源或目标订阅已变化，请重新确认优惠报价。")
	}
	if err := validateCheckoutOffer(in, accepted.Quote, provider, now); err != nil {
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
	if err != nil || policy.Discount == nil || policy.Discount.Version != 1 || policy.Discount.Quote.Offer.Agreement == nil || policy.Discount.Quote.Offer.Agreement.Scope != "initial_only" || commerce.ValidatePurchaseDiscount(commerce.Order(order), policy) != nil {
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

// A replacement's one-order reduction cannot become a recurring agreement or
// overwrite the undiscounted base. Historical unknown offers still fail closed.
func knownOperationPurchaseDiscount(order model.Order) bool {
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil || policy.Discount == nil || policy.Discount.Version != 3 || commerce.ValidateOperationPurchaseDiscount(commerce.Order(order), policy) != nil {
		return false
	}
	_, err = applyPurchaseSnapshot(order, entitlements.GrantPolicy{})
	return err == nil
}
