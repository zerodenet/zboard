package commercestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// This is the repeat-debit authority, not a copy of a per-order quote. Only
// explicitly enumerated stable terms enter it; dates, source head/epoch, quote
// fingerprints and provider response clocks never become continuing consent.
type automaticRecurringApprovalSnapshot struct {
	Redemption           string                            `json:"redemption,omitempty"`
	Version              int                               `json:"version"`
	OwnerID              uint                              `json:"owner_id"`
	SubscriptionID       uint                              `json:"subscription_id"`
	RootOrderID          uint                              `json:"root_order_id"`
	BaseOrderID          uint                              `json:"base_order_id"`
	PlanID               uint                              `json:"plan_id"`
	PlanSKUID            uint                              `json:"plan_sku_id"`
	Currency             string                            `json:"currency"`
	BillingUnit          string                            `json:"billing_unit"`
	BillingValue         int                               `json:"billing_value"`
	RenewalEffect        string                            `json:"renewal_effect"`
	BasePriceMinor       int64                             `json:"base_price_minor"`
	DiscountMinor        int64                             `json:"discount_minor"`
	PayableMinor         int64                             `json:"payable_minor"`
	Renewable            bool                              `json:"renewable"`
	ResetPolicy          int16                             `json:"reset_policy"`
	TrafficCalcMode      int16                             `json:"traffic_calc_mode"`
	FamilyLimit          int                               `json:"family_limit"`
	ValueAllocationModel string                            `json:"value_allocation_model"`
	TrafficBytes         int64                             `json:"traffic_bytes"`
	SpeedLimitMbps       int                               `json:"speed_limit_mbps"`
	DeviceLimit          int                               `json:"device_limit"`
	Provider             commerce.DiscountProviderIdentity `json:"provider"`
	Selection            string                            `json:"selection"`
	Agreement            commerce.DiscountAgreement        `json:"agreement"`
}

func automaticRecurringApproval(order model.Order, accepted checkoutDiscount) (string, string, error) {
	if !hasCheckoutOffer(&accepted) || accepted.Version != 2 || commerce.ValidateDiscountSource(accepted.Source) != nil || accepted.Quote.Offer.Agreement == nil || order.TargetSubscriptionID == nil {
		return "", "", commerce.ErrDiscountQuoteInvalid
	}
	policy, err := commerce.DecodePurchaseSnapshot(order.PurchaseSnapshot)
	if err != nil {
		return "", "", err
	}
	out := automaticRecurringApprovalSnapshot{
		Redemption: accepted.Quote.Offer.Redemption, Version: 2, OwnerID: order.UserID, SubscriptionID: *order.TargetSubscriptionID, RootOrderID: accepted.Source.RootOrderID, BaseOrderID: accepted.Source.BaseOrderID,
		PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, Currency: order.Currency, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, RenewalEffect: order.RenewalEffect,
		BasePriceMinor: order.AmountCents, DiscountMinor: accepted.Quote.Offer.DiscountMinor, PayableMinor: order.AmountCents - accepted.Quote.Offer.DiscountMinor,
		Renewable: policy.Renewable, ResetPolicy: policy.ResetPolicy, TrafficCalcMode: policy.TrafficCalcMode, FamilyLimit: policy.FamilyLimit, ValueAllocationModel: policy.ValueAllocationModel,
		TrafficBytes: order.TrafficBytes, SpeedLimitMbps: order.SpeedLimitMbps, DeviceLimit: order.DeviceLimit, Provider: accepted.Quote.Provider, Selection: accepted.Context.Selection, Agreement: *accepted.Quote.Offer.Agreement,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", "", err
	}
	digest, err := validateAutomaticRecurringApproval(string(raw), order.UserID, *order.TargetSubscriptionID, order.PlanSKUID, accepted.Source.RootOrderID)
	return string(raw), digest, err
}

func validateAutomaticRecurringApproval(raw string, owner, sub, sku, root uint) (string, error) {
	if len(raw) == 0 || len(raw) > commerce.MaxPurchaseSnapshotBytes || !utf8.ValidString(raw) {
		return "", commerce.ErrDiscountQuoteInvalid
	}
	var in automaticRecurringApprovalSnapshot
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		return "", commerce.ErrDiscountQuoteInvalid
	}
	validHash := func(value string) bool {
		b, err := hex.DecodeString(value)
		return err == nil && len(b) == 32 && value == hex.EncodeToString(b)
	}
	terms, err := commerce.DiscountTermsHash(in.Agreement.TermsJSON)
	if (in.Redemption != "" && in.Redemption != commerce.DiscountRedemptionGuarantee) || in.Version != 2 || owner == 0 || sub == 0 || sku == 0 || root == 0 || in.OwnerID != owner || in.SubscriptionID != sub || in.PlanSKUID != sku || in.RootOrderID != root || in.BaseOrderID != root || in.PlanID == 0 ||
		in.BasePriceMinor < 0 || in.DiscountMinor < 0 || in.DiscountMinor > in.BasePriceMinor || in.PayableMinor != in.BasePriceMinor-in.DiscountMinor || len(in.Currency) != 3 || in.BillingValue <= 0 || in.RenewalEffect != "extend_only" || !in.Renewable ||
		in.ResetPolicy < 0 || in.ResetPolicy > 5 || in.TrafficCalcMode < 0 || in.TrafficCalcMode > 2 || in.FamilyLimit < 0 || in.TrafficBytes < 0 || in.SpeedLimitMbps < 0 || in.DeviceLimit < 0 || in.ValueAllocationModel != "uniform_paid_units_v1" ||
		in.Provider.PluginID == "" || len(in.Provider.PluginID) > 160 || !validHash(in.Provider.SigningKeyFingerprint) || !validHash(in.Provider.PackageDigest) || in.Provider.Generation == 0 || in.Provider.ConfigRevision == 0 || len(in.Selection) > 256 ||
		in.Agreement.Scope != "recurring" || in.Agreement.ID == "" || len(in.Agreement.ID) > 128 || in.Agreement.Version == "" || len(in.Agreement.Version) > 64 || err != nil || terms != in.Agreement.TermsHash {
		return "", commerce.ErrDiscountQuoteInvalid
	}
	for _, c := range in.Currency {
		if c < 'A' || c > 'Z' {
			return "", commerce.ErrDiscountQuoteInvalid
		}
	}
	switch in.BillingUnit {
	case "day", "week", "month", "year":
	default:
		return "", commerce.ErrDiscountQuoteInvalid
	}
	canonical, _ := json.Marshal(in)
	if string(canonical) != raw {
		return "", commerce.ErrDiscountQuoteInvalid
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

type automaticDiscountEvaluation struct {
	SKU      uint
	Discount *checkoutDiscount
	Approval string
	Digest   string
}

var errAutomaticConditionsChanged = errors.New("automatic recurring approved conditions changed")

func automaticAccepted(in *automaticDiscountEvaluation) *checkoutDiscount {
	if in == nil {
		return nil
	}
	return in.Discount
}

// Baseline locks end before Quote. The final caller independently acquires its
// canonical financial scope and compares the entire source-bound V2 context.
func (s AutomaticRenewals) evaluateAutomaticDiscount(ctx context.Context, owner, sub, sku uint) (*automaticDiscountEvaluation, error) {
	if sku == 0 {
		var target model.Subscription
		if err := s.DB.WithContext(ctx).Select("id", "plan_sku_id").Where("id = ? AND user_id = ?", sub, owner).First(&target).Error; err != nil {
			return nil, resourceError(err)
		}
		var row model.SubscriptionAutomaticRenewal
		if err := s.DB.WithContext(ctx).Where("subscription_id = ? AND user_id = ?", sub, owner).First(&row).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		sku = row.PlanSKUID
		if sku == 0 {
			sku = target.PlanSKUID
		}
	}
	request := commerce.OrderCreateRequest{PlanSKUID: sku, TargetSubscriptionID: sub}
	scope, err := prepareDiscountSourceScope(s.DB.WithContext(ctx), owner, request)
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
		order, _, err = prepareOrderWithDiscount(tx, owner, request, commerceNow(s.Now), marker)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := &automaticDiscountEvaluation{SKU: sku}
	if marker.Source == nil {
		return out, nil
	}
	var policy commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &policy) != nil {
		return nil, invalidPurchaseSnapshot()
	}
	in, err := commerce.NewRecurringDiscountContext(commerce.Order(order), policy, marker.Source.Selection, marker.Source)
	if err != nil {
		return nil, err
	}
	if order.OrderType != "renewal" || order.BillingUnit == "once" || order.RenewalEffect != "extend_only" || !policy.Renewable {
		return nil, errors.Join(errAutomaticConditionsChanged, quoteError("长期优惠自动续费仅支持已购的可续费同周期延长协议。"))
	}
	accepted := &checkoutDiscount{Version: 2, Source: marker.Source, Context: in}
	out.Discount = accepted
	if s.Discounts == nil {
		return out, commerce.ErrDiscountQuoteUnavailable
	}
	accepted.Quote, err = s.Discounts.Quote(ctx, marker.Source.ProviderID, in)
	if err == nil {
		err = validateCheckoutOffer(in, accepted.Quote, marker.Source.ProviderID, commerceNow(s.Now))
	}

	if err == nil && accepted.Quote.Provider.SigningKeyFingerprint != marker.Source.SigningKeyFingerprint {
		err = errAutomaticConditionsChanged
	}
	if err != nil {
		return out, err
	}
	out.Approval, out.Digest, err = automaticRecurringApproval(order, *accepted)
	return out, err
}

func (s AutomaticRenewals) readAutomaticWithoutOffer(ctx context.Context, owner, sub uint, cause error) (out commerce.AutomaticRenewalView, err error) {
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := ownedAutomaticConsent(tx, owner, sub, false)
		if err != nil {
			return err
		}
		out = automaticRenewalView(row, sub)
		out.Message = "长期优惠暂不可核验，当前未提供可批准的自动续费报价；仍可关闭授权。"
		var invalid *commerce.ValidationError
		if errors.As(cause, &invalid) {
			out.Message = invalid.Message
		}
		if errors.Is(cause, commerce.ErrDiscountConfirmationRequired) {
			out.Message = "长期优惠需要重新明确确认；当前不会自动扣款，仍可关闭授权。"
		}
		if errors.Is(cause, errAutomaticHistoryBound) {
			out.State = "history_review_required"
			out.Message = automaticHistoryReviewMessage
		}
		return nil
	})
	return
}

// A stored V2 row can never enter the V1/full-price branch, even when its
// provider or source is unavailable. Neither execution nor a status read
// manufactures a replacement approval snapshot.
func sameAutomaticPreparation(current, prepared model.SubscriptionAutomaticRenewal) bool {
	return sameAutomaticPendingLink(current, prepared) && current.SubscriptionID == prepared.SubscriptionID && current.UserID == prepared.UserID && current.Enabled && current.Revision == prepared.Revision && current.PlanSKUID == prepared.PlanSKUID && current.RootOrderID == prepared.RootOrderID && current.AgreementVersion == prepared.AgreementVersion && current.AgreementDigest == prepared.AgreementDigest && current.ApprovalSnapshot == prepared.ApprovalSnapshot && current.NextAttemptAt.Equal(prepared.NextAttemptAt) && current.AttemptSequence == prepared.AttemptSequence
}
