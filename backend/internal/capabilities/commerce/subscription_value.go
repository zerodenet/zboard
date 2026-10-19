package commerce

import (
	"context"
	"errors"
)

var ErrSubscriptionValueCyclePending = errors.New("subscription quota cycle reset is pending")

var ErrSubscriptionValueUnavailable = errors.New("subscription value requires purchase/payment reconciliation")

type SubscriptionValueQuote struct {
	SubscriptionID uint   `json:"subscription_id"`
	Currency       string `json:"currency"`
	RootOrderID    uint   `json:"root_order_id"`
	CurrentMinor   int64  `json:"current_minor,string"`
	FutureMinor    int64  `json:"future_minor,string"`
	TotalMinor     int64  `json:"total_minor,string"`
	Fingerprint    string `json:"quote_fingerprint"`
}
type SubscriptionValuesRepository interface {
	Read(context.Context, uint, uint) (SubscriptionValueQuote, error)
}
type SubscriptionValues struct{ Repository SubscriptionValuesRepository }

func (s SubscriptionValues) Read(ctx context.Context, actor, subscription uint) (SubscriptionValueQuote, error) {
	if actor == 0 {
		return SubscriptionValueQuote{}, ErrOrderPermission
	}
	if subscription == 0 {
		return SubscriptionValueQuote{}, ErrNotFound
	}
	if s.Repository == nil {
		return SubscriptionValueQuote{}, ErrSubscriptionValueUnavailable
	}
	return s.Repository.Read(ctx, actor, subscription)
}
