package commerce

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrDiscountConfirmationRequired = errors.New("discount agreement requires explicit confirmation")

// NewRecurringDiscountContext uses a separate V2 source domain for renewal.
// A new recurring purchase shares the V1 host-facts input representation; its
// evidence version and returned scope are bound in the final purchase quote.
// Existing V1 initial purchase fingerprints remain byte-for-byte compatible.
func NewRecurringDiscountContext(order Order, policy PurchaseSnapshot, selection string, source *DiscountSourceSnapshot) (DiscountQuoteContext, error) {
	if policy.Version != 1 || policy.BasePriceMinor != order.AmountCents || policy.Currency != order.Currency || policy.BillingUnit != order.BillingUnit || policy.BillingValue != order.BillingValue || policy.RenewalEffect != order.RenewalEffect {
		return DiscountQuoteContext{}, ErrDiscountQuoteInvalid
	}
	in := DiscountQuoteContext{Version: 1, BuyerID: order.UserID, Operation: order.OrderType, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, BasePriceMinor: order.AmountCents, Currency: order.Currency, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, Selection: selection}
	switch order.OrderType {
	case "new":
		if order.TargetSubscriptionID != nil || source != nil {
			return in, ErrDiscountQuoteInvalid
		}
		return NewPurchaseDiscountContext(order, policy, selection)
	case "renewal":
		if ValidateDiscountSource(source) != nil || order.TargetSubscriptionID == nil || *order.TargetSubscriptionID != source.SubscriptionID || policy.RenewalTarget == nil || selection != source.Selection {
			return in, ErrDiscountQuoteInvalid
		}
		in.TargetSubscriptionID = source.SubscriptionID
		in.PriceSourceOrderID = source.BaseOrderID
		agreement := source.Agreement
		in.Agreement = &agreement
	default:
		return in, ErrDiscountQuoteInvalid
	}
	policy.Discount = nil
	raw, err := json.Marshal(struct {
		EvidenceVersion                  int
		Context                          DiscountQuoteContext
		Policy                           PurchaseSnapshot
		Source                           *DiscountSourceSnapshot
		PlanName, SKUName, RenewalEffect string
		Traffic                          int64
		Device, Speed                    int
	}{2, in, policy, source, order.PlanName, order.SKUName, order.RenewalEffect, order.TrafficBytes, order.DeviceLimit, order.SpeedLimitMbps})
	if err != nil {
		return in, ErrDiscountQuoteInvalid
	}
	digest := sha256.Sum256(raw)
	in.InputFingerprint = hex.EncodeToString(digest[:])
	return in, ValidateDiscountQuoteContext(in)
}

func ValidateRecurringPurchaseDiscount(order Order, policy PurchaseSnapshot) error {
	saved := policy.Discount
	if saved == nil || saved.Version != 2 || saved.AcceptedAt.IsZero() || saved.Quote.Offer.Status != "available" || saved.Quote.Offer.Agreement == nil || saved.Quote.Offer.Agreement.Scope != "recurring" || ValidateDiscountQuote(saved.Context, saved.Quote, saved.AcceptedAt) != nil {
		return ErrDiscountQuoteInvalid
	}
	current, err := NewRecurringDiscountContext(order, policy, saved.Context.Selection, saved.Source)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(saved.Context)
	if string(a) != string(b) || order.DiscountAmount != saved.Quote.Offer.DiscountMinor || order.PayableAmount != saved.FinalPriceMinor || saved.FinalPriceMinor != policy.BasePriceMinor-saved.Quote.Offer.DiscountMinor {
		return ErrDiscountQuoteInvalid
	}
	if saved.Source != nil {
		a, _ := json.Marshal(saved.Quote.Offer.Agreement)
		b, _ := json.Marshal(saved.Source.Agreement)
		if string(a) != string(b) {
			return ErrDiscountQuoteInvalid
		}
	}
	if saved.Source != nil && (saved.Quote.Provider.PluginID != saved.Source.ProviderID || saved.Quote.Provider.SigningKeyFingerprint != saved.Source.SigningKeyFingerprint) {
		return ErrDiscountQuoteInvalid
	}
	return nil
}
