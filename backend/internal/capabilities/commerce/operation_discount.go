package commerce

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"
)

const OperationDiscountScope = "order_only"

// operationDiscountTarget strips only quote-time and discount-dependent credit
// allocation. It retains the entire preexisting target/source monetary model.
// Final fulfillment separately revalidates actual sources and target state.
func operationDiscountTarget(order Order) (*SubscriptionChangeSnapshot, error) {
	if order.OrderType == "traffic_pack" && order.ChangeSnapshot == "" {
		return nil, nil
	}
	if len(order.ChangeSnapshot) == 0 || len(order.ChangeSnapshot) > 256<<10 {
		return nil, ErrDiscountQuoteInvalid
	}
	var value SubscriptionChangeSnapshot
	decoder := json.NewDecoder(bytes.NewBufferString(order.ChangeSnapshot))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.PlanID == 0 || value.EndAt.IsZero() || value.QuotedAt.IsZero() {
		return nil, ErrDiscountQuoteInvalid
	}
	if order.OrderType == "upgrade" {
		// The serialized target is already bounded above. Source cardinality
		// must not reject an otherwise valid paid history; the core binds every
		// exact source in its fingerprint and rechecks values at fulfillment.
		if value.Version != 2 || !value.FullTerm || value.Residual < 0 || value.Applied != min(value.Residual, order.PayableAmount) || value.Surplus != value.Residual-value.Applied || value.Credit != value.Applied {
			return nil, ErrDiscountQuoteInvalid
		}
	} else if order.OrderType == "traffic_reset" {
		if value.Version != 0 || value.FullTerm || value.Credit != 0 {
			return nil, ErrDiscountQuoteInvalid
		}
	} else {
		return nil, ErrDiscountQuoteInvalid
	}
	value.QuotedAt = time.Time{}
	value.ValueFingerprint = ""
	value.Applied = 0
	value.Surplus = 0
	value.Credit = 0
	return &value, nil
}

// NewOperationDiscountContext is only for an explicitly selected one-order
// offer on a supported target operation. It does not inherit, replace or create
// renewal consent and never reinterprets initial_only as an add-on discount.
func NewOperationDiscountContext(order Order, policy PurchaseSnapshot, selection string) (DiscountQuoteContext, error) {
	if (order.OrderType != "upgrade" && order.OrderType != "traffic_reset" && order.OrderType != "traffic_pack") || order.TargetSubscriptionID == nil || *order.TargetSubscriptionID == 0 || order.AmountCents < 0 || order.PayableAmount < 0 || policy.Version != 1 || policy.BasePriceMinor != order.AmountCents || policy.Currency != order.Currency || policy.BillingUnit != order.BillingUnit || policy.BillingValue != order.BillingValue || policy.RenewalEffect != order.RenewalEffect {
		return DiscountQuoteContext{}, ErrDiscountQuoteInvalid
	}
	target, err := operationDiscountTarget(order)
	if err != nil {
		return DiscountQuoteContext{}, err
	}
	policy.Discount = nil
	in := DiscountQuoteContext{Version: 1, BuyerID: order.UserID, Operation: order.OrderType, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, TargetSubscriptionID: *order.TargetSubscriptionID, BasePriceMinor: order.AmountCents, Currency: order.Currency, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, Selection: selection}
	if target != nil {
		in.PriceSourceOrderID = target.SourceOrderID
	}
	raw, err := json.Marshal(struct {
		Purpose                          string
		Context                          DiscountQuoteContext
		Policy                           PurchaseSnapshot
		Target                           *SubscriptionChangeSnapshot
		PlanName, SKUName, RenewalEffect string
		Traffic                          int64
		Device, Speed                    int
	}{"operation_discount_v3", in, policy, target, order.PlanName, order.SKUName, order.RenewalEffect, order.TrafficBytes, order.DeviceLimit, order.SpeedLimitMbps})
	if err != nil {
		return in, ErrDiscountQuoteInvalid
	}
	digest := sha256.Sum256(raw)
	in.InputFingerprint = hex.EncodeToString(digest[:])
	return in, ValidateDiscountQuoteContext(in)
}

func ValidateOperationPurchaseDiscount(order Order, policy PurchaseSnapshot) error {
	saved := policy.Discount
	if saved == nil || saved.Version != 3 || saved.Source != nil || saved.AcceptedAt.IsZero() || saved.Quote.Offer.Status != "available" || saved.Quote.Offer.Agreement == nil || saved.Quote.Offer.Agreement.Scope != OperationDiscountScope || ValidateDiscountQuote(saved.Context, saved.Quote, saved.AcceptedAt) != nil {
		return ErrDiscountQuoteInvalid
	}
	current, err := NewOperationDiscountContext(order, policy, saved.Context.Selection)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(saved.Context)
	if string(a) != string(b) || saved.Quote.Offer.DiscountMinor != order.DiscountAmount || saved.FinalPriceMinor != order.PayableAmount || saved.FinalPriceMinor != policy.BasePriceMinor-saved.Quote.Offer.DiscountMinor {
		return ErrDiscountQuoteInvalid
	}
	return nil
}
