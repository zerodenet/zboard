package commerce

import (
	"context"
	"encoding/hex"
	"time"
)

// Automatic renewal is a subscription-scoped, explicit wallet-only agreement.
// The browser confirms a server quote; it never supplies a charge amount.
type AutomaticRenewalRequest struct {
	Enabled          bool   `json:"enabled"`
	PlanSKUID        uint   `json:"plan_sku_id"`
	QuoteFingerprint string `json:"quote_fingerprint"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

type AutomaticRenewalOffer struct {
	PlanSKUID        uint   `json:"plan_sku_id"`
	Currency         string `json:"currency"`
	BasePriceMinor   int64  `json:"base_price_minor,string"`
	BillingUnit      string `json:"billing_unit"`
	BillingValue     int    `json:"billing_value"`
	QuoteFingerprint string `json:"quote_fingerprint"`
}

type AutomaticRenewalView struct {
	SubscriptionID uint                   `json:"subscription_id"`
	Enabled        bool                   `json:"enabled"`
	PlanSKUID      uint                   `json:"plan_sku_id"`
	Revision       uint64                 `json:"revision"`
	State          string                 `json:"state"`
	Message        string                 `json:"message"`
	LastOrderID    *uint                  `json:"last_order_id,omitempty"`
	NextAttemptAt  *time.Time             `json:"next_attempt_at,omitempty"`
	Offer          *AutomaticRenewalOffer `json:"offer,omitempty"`
}

type AutomaticRenewalsRepository interface {
	Read(context.Context, uint, uint, uint) (AutomaticRenewalView, error)
	Set(context.Context, uint, uint, AutomaticRenewalRequest) (AutomaticRenewalView, error)
	Process(context.Context, int) (int, error)
}

type AutomaticRenewals struct{ Repository AutomaticRenewalsRepository }

func (s AutomaticRenewals) Read(ctx context.Context, actor, subscription, sku uint) (AutomaticRenewalView, error) {
	if actor == 0 || subscription == 0 {
		return AutomaticRenewalView{}, ErrOrderPermission
	}
	return s.Repository.Read(ctx, actor, subscription, sku)
}

func (s AutomaticRenewals) Set(ctx context.Context, actor, subscription uint, in AutomaticRenewalRequest) (AutomaticRenewalView, error) {
	if actor == 0 || subscription == 0 {
		return AutomaticRenewalView{}, ErrOrderPermission
	}
	if in.Enabled {
		fingerprint, err := hex.DecodeString(in.QuoteFingerprint)
		if in.PlanSKUID == 0 || err != nil || len(fingerprint) != 32 {
			return AutomaticRenewalView{}, &ValidationError{Message: "请先核对该订阅的续费规格和锁价，再开启余额自动续费。"}
		}
	}
	return s.Repository.Set(ctx, actor, subscription, in)
}

func (s AutomaticRenewals) Process(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, &ValidationError{Message: "自动续费批次必须在1至200之间。"}
	}
	return s.Repository.Process(ctx, limit)
}
