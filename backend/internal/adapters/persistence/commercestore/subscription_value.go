package commercestore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
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
	records, sources, currency, err := subscriptionValueRecords(subscriptionValueOrderQuery(tx, sub), sub)
	if err != nil {
		return commerce.ValueProjection{}, "", err
	}
	var projection commerce.ValueProjection
	if entitlements.IsPerpetualEnd(sub.EndAt) {
		events, _, readErr := readPlanBaseQuotaSyncEvents(tx, sub, records, false)
		if readErr != nil {
			return commerce.ValueProjection{}, currency, readErr
		}
		projection, err = commerce.ProjectEntitlementValueWithBaseSyncContext(tx.Statement.Context, records, events, entitlementValueState(sub), currency, at, sub.EntitlementEpoch)
	} else {
		events, _, readErr := readTimedPlanQuotaSyncEvents(tx, sub, records, false)
		if readErr != nil {
			return commerce.ValueProjection{}, currency, readErr
		}
		projection, err = commerce.ProjectTimedEntitlementValueWithPlanSyncContext(tx.Statement.Context, records, events, entitlementValueState(sub), currency, at, sub.EntitlementEpoch)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return commerce.ValueProjection{}, currency, err
		}
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
