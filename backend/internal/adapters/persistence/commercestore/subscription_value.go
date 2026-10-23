package commercestore

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type SubscriptionValues struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s SubscriptionValues) Read(ctx context.Context, actor, id uint) (out commerce.SubscriptionValueQuote, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, false); err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.Where("id = ? AND user_id = ?", id, actor).First(&sub).Error; err != nil {
			return resourceError(err)
		}
		if sub.Status != "active" && sub.Status != "expired" {
			return commerce.ErrSubscriptionValueUnavailable
		}
		projection, currency, err := readSubscriptionValue(tx, sub, commerceNow(s.Now))
		if err != nil {
			return err
		}
		out = commerce.SubscriptionValueQuote{SubscriptionID: sub.ID, Currency: currency, RootOrderID: projection.RootOrderID, CurrentMinor: projection.CurrentMinor, FutureMinor: projection.FutureMinor, TotalMinor: projection.TotalMinor, Fingerprint: projection.Fingerprint}
		return nil
	})
	return
}

// Called within the owning transaction. It deliberately reads authoritative
// orders and current quota counters, never per-flow diagnostics or charts.
func readSubscriptionValue(tx *gorm.DB, sub model.Subscription, at time.Time) (commerce.ValueProjection, string, error) {
	if sub.NextResetAt != nil && !sub.NextResetAt.After(at) && sub.EndAt.After(at) {
		return commerce.ValueProjection{}, "", commerce.ErrSubscriptionValueCyclePending
	}
	var orders []model.Order
	err := tx.Select("id", "paid_amount", "refund_amount", "currency", "entitlement_value_snapshot", "subscription_ended_at").Where("subscription_id = ? AND status = ? AND (subscription_ended_at IS NULL OR subscription_ended_at > ?)", sub.ID, "paid", sub.StartAt).Order("id ASC").Limit(1001).Find(&orders).Error
	if err != nil {
		return commerce.ValueProjection{}, "", err
	}
	if len(orders) == 0 || len(orders) > 1000 {
		return commerce.ValueProjection{}, "", commerce.ErrSubscriptionValueUnavailable
	}
	records := make([]commerce.EntitlementValueSnapshot, 0, len(orders))
	currency := ""
	sources := map[uint]model.Order{}
	for _, order := range orders {
		var fact commerce.EntitlementValueSnapshot
		if json.Unmarshal([]byte(order.EntitlementValueSnapshot), &fact) != nil || fact.OrderID != order.ID || fact.SubscriptionID != sub.ID || fact.ActualPaidMinor != order.PaidAmount || fact.Currency != order.Currency || order.RefundAmount != 0 {
			return commerce.ValueProjection{}, "", commerce.ErrSubscriptionValueUnavailable
		}
		if currency == "" {
			currency = order.Currency
		}
		if currency != order.Currency {
			return commerce.ValueProjection{}, "", commerce.ErrSubscriptionValueUnavailable
		}
		records = append(records, fact)
		sources[order.ID] = order
	}
	projection, err := commerce.ProjectEntitlementValue(records, entitlementValueState(sub), currency, at)
	if err != nil {
		return commerce.ValueProjection{}, currency, fmt.Errorf("%w: %v", commerce.ErrSubscriptionValueUnavailable, err)
	}
	for id, amount := range projection.Sources {
		order, ok := sources[id]
		if !ok || amount < 0 || amount > order.PaidAmount {
			return commerce.ValueProjection{}, currency, commerce.ErrSubscriptionValueUnavailable
		}
	}
	return projection, currency, nil
}
