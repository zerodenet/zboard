package commerce

import (
	"context"
	"errors"
	"time"
)

var ErrFundingConflict = errors.New("order funding conflict")

type FundingRequest struct {
	UseBalance bool `json:"use_balance"`
}
type OrderFundingView struct {
	DiscountReservation *DiscountReservationView `json:"discount_reservation,omitempty"`
	ChangeMode          string                   `json:"change_mode,omitempty"`
	SettlementMinor     int64                    `json:"settlement_minor,string"`
	SurplusMinor        int64                    `json:"surplus_minor,string"`
	EntitlementMinor    int64                    `json:"entitlement_minor,string"`
	ReservedMinor       int64                    `json:"reserved_minor,string"`
	OrderID             uint                     `json:"order_id"`
	Configured          bool                     `json:"configured"`
	UseBalance          bool                     `json:"use_balance"`
	Currency            string                   `json:"currency"`
	AmountMinor         int64                    `json:"amount_minor,string"`
	AvailableMinor      int64                    `json:"available_minor,string"`
	WalletMinor         int64                    `json:"wallet_minor,string"`
	CashMinor           int64                    `json:"cash_minor,string"`
	State               string                   `json:"state"`
	ExpiresAt           *time.Time               `json:"expires_at,omitempty"`
	Expired             bool                     `json:"expired"`
	ReleaseReason       string                   `json:"release_reason,omitempty"`
}
type WalletPaymentResult struct {
	OrderID        uint   `json:"order_id"`
	SubscriptionID uint   `json:"subscription_id"`
	Status         string `json:"status"`
	Fulfilled      bool   `json:"fulfilled"`
}
type WalletPaymentsRepository interface {
	Read(context.Context, uint, uint) (OrderFundingView, error)
	Configure(context.Context, uint, uint, FundingRequest) (OrderFundingView, error)
	Pay(context.Context, uint, uint) (WalletPaymentResult, error)
	Expire(context.Context, time.Time, int) (int, error)
}
type WalletPayments struct{ Repository WalletPaymentsRepository }

func (s WalletPayments) Read(ctx context.Context, actor, order uint) (OrderFundingView, error) {
	if actor == 0 {
		return OrderFundingView{}, ErrOrderPermission
	}
	if order == 0 {
		return OrderFundingView{}, ErrNotFound
	}
	return s.Repository.Read(ctx, actor, order)
}
func (s WalletPayments) Configure(ctx context.Context, actor, order uint, in FundingRequest) (OrderFundingView, error) {
	if actor == 0 {
		return OrderFundingView{}, ErrOrderPermission
	}
	if order == 0 {
		return OrderFundingView{}, ErrNotFound
	}
	return s.Repository.Configure(ctx, actor, order, in)
}
func (s WalletPayments) Pay(ctx context.Context, actor, order uint) (WalletPaymentResult, error) {
	if actor == 0 {
		return WalletPaymentResult{}, ErrOrderPermission
	}
	if order == 0 {
		return WalletPaymentResult{}, ErrNotFound
	}
	return s.Repository.Pay(ctx, actor, order)
}
func (s WalletPayments) Expire(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() || limit < 1 || limit > 200 {
		return 0, &ValidationError{Message: "资金预留到期处理参数无效。"}
	}
	return s.Repository.Expire(ctx, now.UTC(), limit)
}
