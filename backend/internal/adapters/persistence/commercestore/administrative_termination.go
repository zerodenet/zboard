package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func (s SubscriptionValues) ReadForAdministrator(ctx context.Context, actor, id uint) (out commerce.AdministrativeSubscriptionValueQuote, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.First(&sub, id).Error; err != nil {
			return resourceError(err)
		}
		if sub.Status != "active" && sub.Status != "expired" {
			return commerce.ErrSubscriptionValueUnavailable
		}
		var valueErr error
		out, _, _, valueErr = prepareAdministrativeValue(tx, sub, commerceNow(s.Now))
		if valueErr != nil {
			return valueErr
		}
		return nil
	})
	return
}

// Read only: a quote never creates an opening budget. Every surviving budget
// must agree with the immutable actual-paid source; unknown cash is not a limit.
func administrativeRefundLimits(tx *gorm.DB, sub model.Subscription, value commerce.ValueProjection, currency string) (map[uint]int64, int64, error) {
	orders, err := readOrderEvidence(tx.Model(&model.Order{}).Where("subscription_id = ? AND status = ? AND (subscription_ended_at IS NULL OR subscription_ended_at > ?)", sub.ID, "paid", sub.StartAt), "id", "user_id", "subscription_id", "status", "order_type", "paid_amount", "refund_amount", "currency", "fulfilled_at", "entitlement_value_snapshot")
	if err != nil {
		return nil, 0, err
	}
	if len(orders) == 0 {
		return nil, 0, commerce.ErrSubscriptionValueUnavailable
	}
	var rootEvent uint
	for _, order := range orders {
		if order.ID == value.RootOrderID {
			var fact commerce.EntitlementValueSnapshot
			if json.Unmarshal([]byte(order.EntitlementValueSnapshot), &fact) != nil {
				return nil, 0, commerce.ErrSubscriptionValueUnavailable
			}
			rootEvent = fact.QuotaEventID
		}
	}
	if rootEvent == 0 {
		return nil, 0, commerce.ErrSubscriptionValueUnavailable
	}
	ids := make([]uint, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
	}
	var budgetRows []model.OrderValueBudget
	for first := 0; first < len(ids); first += orderEvidencePageSize {
		var page []model.OrderValueBudget
		if err := tx.Where("order_id IN ?", ids[first:min(first+orderEvidencePageSize, len(ids))]).Find(&page).Error; err != nil {
			return nil, 0, err
		}
		budgetRows = append(budgetRows, page...)
	}
	budgets := map[uint]model.OrderValueBudget{}
	for _, row := range budgetRows {
		budgets[row.OrderID] = row
	}
	missing := make([]uint, 0)
	for _, id := range ids {
		if _, ok := budgets[id]; !ok {
			missing = append(missing, id)
		}
	}
	for first := 0; first < len(missing); first += orderEvidencePageSize {
		var surviving int64
		if err := tx.Model(&model.SubscriptionValueSettlementSource{}).Where("order_id IN ?", missing[first:min(first+orderEvidencePageSize, len(missing))]).Count(&surviving).Error; err != nil {
			return nil, 0, err
		}
		if surviving > 0 {
			return nil, 0, commerce.ErrSubscriptionValueUnavailable
		}
	}
	limits := map[uint]int64{}
	var total int64
	for _, order := range orders {
		var fact commerce.EntitlementValueSnapshot
		if json.Unmarshal([]byte(order.EntitlementValueSnapshot), &fact) != nil {
			return nil, 0, commerce.ErrSubscriptionValueUnavailable
		}
		if fact.QuotaEventID < rootEvent {
			continue
		}
		if order.OrderType == "topup" || order.FulfilledAt == nil || order.UserID != sub.UserID || order.Currency != currency || !fact.PaymentVerified || fact.OrderID != order.ID || fact.SubscriptionID != sub.ID || fact.Currency != currency || fact.ActualPaidMinor != order.PaidAmount || order.PaidAmount < 0 || order.RefundAmount != 0 {
			return nil, 0, commerce.ErrSubscriptionValueUnavailable
		}
		available := order.PaidAmount
		if budget, ok := budgets[order.ID]; ok {
			if budget.UserID != sub.UserID || budget.Currency != currency || budget.FundingMinor != order.PaidAmount || budget.FundingFingerprint != walletKey(order.EntitlementValueSnapshot) || budget.RefundedMinor != order.RefundAmount || budget.RefundedMinor < 0 || budget.ConvertedMinor < 0 || budget.RefundedMinor > budget.FundingMinor || budget.ConvertedMinor > budget.FundingMinor-budget.RefundedMinor {
				return nil, 0, commerce.ErrSubscriptionSettlementConflict
			}
			available -= budget.RefundedMinor + budget.ConvertedMinor
		}
		if total > math.MaxInt64-available {
			return nil, 0, commerce.ErrWalletRange
		}
		total += available
		limits[order.ID] = available
	}
	if value.TotalMinor > total {
		return nil, 0, commerce.ErrSubscriptionSettlementConflict
	}
	return limits, total, nil
}

func administrativeValueFingerprint(defaultFingerprint string, limits map[uint]int64, maximum int64) string {
	raw, _ := json.Marshal(struct {
		Default string
		Limits  map[uint]int64
		Maximum int64
	}{defaultFingerprint, limits, maximum})
	return walletKey(string(raw))
}

func prepareAdministrativeValue(tx *gorm.DB, sub model.Subscription, now time.Time) (commerce.AdministrativeSubscriptionValueQuote, commerce.ValueProjection, map[uint]int64, error) {
	value, currency, err := readSubscriptionValue(tx, sub, now)
	var limits map[uint]int64
	var maximum int64
	if err == nil {
		limits, maximum, err = administrativeRefundLimits(tx, sub, value, currency)
	}
	if err != nil {
		if errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
			quote, budgetErr := unpricedBudgetQuote(tx, sub)
			return quote, value, nil, budgetErr
		}
		if !errors.Is(err, commerce.ErrSubscriptionValueUnavailable) && !errors.Is(err, commerce.ErrSubscriptionValueCyclePending) {
			return commerce.AdministrativeSubscriptionValueQuote{}, value, nil, err
		}
		orders, loadErr := readOrderEvidence(terminationOrders(tx, sub.UserID, sub.ID, sub.StartAt))
		if loadErr != nil {
			return commerce.AdministrativeSubscriptionValueQuote{}, value, nil, loadErr
		}
		reason := "historical_value_unavailable"
		if errors.Is(err, commerce.ErrSubscriptionValueCyclePending) {
			reason = "quota_reset_pending"
		}
		fp := unpricedTerminationFingerprint(sub, orders, reason)
		return commerce.AdministrativeSubscriptionValueQuote{SubscriptionID: sub.ID, UserID: sub.UserID, Fingerprint: fp, ValuationStatus: "unavailable", Message: "默认残值及可退实付尚无法核对；只能明确输入0元终止权益，未将未知价值视为0。"}, value, nil, nil
	}
	return commerce.AdministrativeSubscriptionValueQuote{SubscriptionID: sub.ID, UserID: sub.UserID, Currency: currency, RootOrderID: &value.RootOrderID, CurrentMinor: &value.CurrentMinor, FutureMinor: &value.FutureMinor, TotalMinor: &value.TotalMinor, Fingerprint: administrativeValueFingerprint(value.Fingerprint, limits, maximum), MaxRefundMinor: maximum, ValuationStatus: "verified"}, value, limits, nil
}

func unpricedTerminationFingerprint(sub model.Subscription, orders []model.Order, reason string) string {
	type orderBoundary struct {
		ID             uint
		Status         string
		SubscriptionID uint
		Target         *uint
		Paid, Refund   int64
		Evidence       string
		EndedAt        *time.Time
	}
	rows := make([]orderBoundary, 0, len(orders))
	for _, order := range orders {
		rows = append(rows, orderBoundary{order.ID, order.Status, order.SubscriptionID, order.TargetSubscriptionID, order.PaidAmount, order.RefundAmount, walletKey(order.PurchaseSnapshot + "\x00" + order.EntitlementValueSnapshot), order.SubscriptionEndedAt})
	}
	raw, _ := json.Marshal(struct {
		Model        string
		Subscription uint
		Owner        uint
		Group        uint
		Epoch        uint64
		State        commerce.EntitlementValueState
		Reason       string
		Orders       []orderBoundary
	}{"admin_zero_unpriced_v1", sub.ID, sub.UserID, sub.NodeGroupID, sub.EntitlementEpoch, entitlementValueState(sub), reason, rows})
	return walletKey(string(raw))
}
