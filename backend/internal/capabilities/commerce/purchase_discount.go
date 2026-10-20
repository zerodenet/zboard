package commerce

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"
)

// PurchaseDiscountSnapshot records acceptance, independently from payment and
// entitlement value. Base, reduction and final price retain distinct meanings.
type PurchaseDiscountSnapshot struct {
	Version         int                  `json:"version"`
	Context         DiscountQuoteContext `json:"context"`
	Quote           DiscountQuote        `json:"quote"`
	FinalPriceMinor int64                `json:"final_price_minor"`
	AcceptedAt      time.Time            `json:"accepted_at"`
}

// NewPurchaseDiscountContext binds all current commercial facts, excluding
// generated trade numbers, observations and previous plugin responses. Payment
// channel is deliberately absent: funding may change it to wallet after the
// commercial agreement is locked, without changing its base or discount. Only
// new purchases are admitted by this checkout version.
func NewPurchaseDiscountContext(order Order, policy PurchaseSnapshot, selection string) (DiscountQuoteContext, error) {
	if order.OrderType != "new" || order.TargetSubscriptionID != nil || policy.Version != 1 ||
		policy.BasePriceMinor != order.AmountCents || policy.Currency != order.Currency ||
		policy.BillingUnit != order.BillingUnit || policy.BillingValue != order.BillingValue || policy.RenewalEffect != order.RenewalEffect {
		return DiscountQuoteContext{}, ErrDiscountQuoteInvalid
	}
	policy.Discount = nil
	in := DiscountQuoteContext{Version: 1, BuyerID: order.UserID, Operation: order.OrderType, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, BasePriceMinor: order.AmountCents, Currency: order.Currency, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, Selection: selection}
	raw, err := json.Marshal(struct {
		Context                          DiscountQuoteContext
		Policy                           PurchaseSnapshot
		PlanName, SKUName, RenewalEffect string
		Traffic                          int64
		Device, Speed                    int
	}{in, policy, order.PlanName, order.SKUName, order.RenewalEffect, order.TrafficBytes, order.DeviceLimit, order.SpeedLimitMbps})
	if err != nil {
		return in, ErrDiscountQuoteInvalid
	}
	digest := sha256.Sum256(raw)
	in.InputFingerprint = hex.EncodeToString(digest[:])
	return in, ValidateDiscountQuoteContext(in)
}

// ValidatePurchaseDiscount verifies stored acceptance at its original time;
// expiration prevents accepting a new offer, not paying an already locked order.
// Presence of an arbitrary discount field is never renewal authority.
func ValidatePurchaseDiscount(order Order, policy PurchaseSnapshot) error {
	saved := policy.Discount
	if saved == nil || saved.Version != 1 || saved.AcceptedAt.IsZero() || saved.Quote.Offer.Status != "available" ||
		saved.Quote.Offer.Agreement == nil || saved.Quote.Offer.Agreement.Scope != "initial_only" ||
		ValidateDiscountQuote(saved.Context, saved.Quote, saved.AcceptedAt) != nil {
		return ErrDiscountQuoteInvalid
	}
	current, err := NewPurchaseDiscountContext(order, policy, saved.Context.Selection)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(saved.Context)
	if string(a) != string(b) || saved.Quote.Offer.DiscountMinor != order.DiscountAmount ||
		saved.FinalPriceMinor != order.PayableAmount || saved.FinalPriceMinor != policy.BasePriceMinor-saved.Quote.Offer.DiscountMinor {
		return ErrDiscountQuoteInvalid
	}
	return nil
}

// PurchaseQuotePolicy strips only per-evaluation clock/explanation values.
// Identity, package, signer, generation, configuration, input, agreement and
// every monetary value remain bound, so a fresh equivalent offer can confirm a
// preview without requiring identical expiry timestamps from repeated RPCs.
func PurchaseQuotePolicy(policy PurchaseSnapshot) PurchaseSnapshot {
	if policy.Discount != nil {
		saved := *policy.Discount
		saved.AcceptedAt = time.Time{}
		saved.Quote.Offer.ExpiresAt = time.Time{}
		saved.Quote.Offer.Message = ""
		policy.Discount = &saved
	}
	return policy
}

// DecodePurchaseSnapshot is strict for accepted discount evidence, including
// nested context, provider, offer and agreement fields. Legacy orders without
// discount evidence retain their separate compatibility path.
func DecodePurchaseSnapshot(raw string) (PurchaseSnapshot, error) {
	var out PurchaseSnapshot
	if len(raw) == 0 || len(raw) > MaxPurchaseSnapshotBytes || !utf8.ValidString(raw) {
		return out, ErrDiscountQuoteInvalid
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || out.Version != 1 {
		return out, ErrDiscountQuoteInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return out, ErrDiscountQuoteInvalid
	}
	return out, nil
}
