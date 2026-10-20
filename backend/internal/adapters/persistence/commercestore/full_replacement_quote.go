package commercestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func fullReplacementSnapshot(order model.Order) (*changeSnapshot, error) {
	if order.OrderType != "upgrade" || order.ChangeSnapshot == "" {
		return nil, nil
	}
	var snapshot changeSnapshot
	if json.Unmarshal([]byte(order.ChangeSnapshot), &snapshot) != nil {
		return nil, quoteError("换购报价依据无法读取，请核对订单。")
	}
	if snapshot.Version == 0 && !snapshot.FullTerm {
		return nil, nil
	}
	if snapshot.Version != 2 || !snapshot.FullTerm || order.TargetSubscriptionID == nil || snapshot.QuotedAt.IsZero() || snapshot.Residual < 0 || snapshot.Applied < 0 || snapshot.Applied > snapshot.Residual || snapshot.Surplus != snapshot.Residual-snapshot.Applied || snapshot.Applied != min(snapshot.Residual, order.PayableAmount) {
		return nil, quoteError("完整换购报价依据无效，请核对订单。")
	}
	return &snapshot, nil
}

func quoteFullReplacement(tx *gorm.DB, order *model.Order, sub model.Subscription, now, creditAt time.Time) (commerce.OrderPreview, error) {
	empty := commerce.OrderPreview{}
	if order.OrderType != "upgrade" || order.TargetSubscriptionID == nil || *order.TargetSubscriptionID != sub.ID || order.UserID != sub.UserID || !entitlements.CanChangeAt(entitlements.Subscription(sub), now) {
		return empty, quoteError("请选择尚未结束且允许换购的订阅。")
	}
	if order.PayableAmount < 0 || creditAt.IsZero() || creditAt.After(now) || sub.NextResetAt != nil && !sub.NextResetAt.After(now) {
		return empty, quoteError("权益周期正在变化，请刷新后确认换购。")
	}
	value, currency, err := readSubscriptionValue(tx, sub, creditAt)
	if err != nil {
		if errors.Is(err, commerce.ErrSubscriptionValueUnavailable) || errors.Is(err, commerce.ErrSubscriptionValueCyclePending) {
			return empty, quoteError("原订阅的实付、周期或权益依据待核对，暂不能自动换购。")
		}
		return empty, err
	}
	if currency != order.Currency {
		return empty, quoteError("不同币种的订阅不能自动换购结算。")
	}
	end, err := entitlements.AddBillingPeriod(now, order.BillingUnit, order.BillingValue)
	if err != nil {
		return empty, err
	}
	applied := min(value.TotalMinor, order.PayableAmount)
	snapshot := changeSnapshot{Version: 2, FullTerm: true, EntitlementEpoch: sub.EntitlementEpoch, PlanID: sub.PlanID, PlanSKUID: sub.PlanSKUID, EndAt: sub.EndAt, CycleStartUsed: sub.CycleStartUsed, ResetQuota: sub.ResetQuotaBytes, SourceOrderID: value.RootOrderID, QuotedAt: creditAt, Credit: applied, Residual: value.TotalMinor, Applied: applied, Surplus: value.TotalMinor - applied, ValueFingerprint: value.Fingerprint, Sources: value.Sources}
	if err := tx.Model(&model.QuotaEvent{}).Where("subscription_id = ?", sub.ID).Select("COALESCE(MAX(id),0)").Scan(&snapshot.QuotaEventID).Error; err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return empty, err
	}
	order.ChangeSnapshot = string(encoded)
	preview := commerce.OrderPreview{ChangeMode: "full_term", OrderType: order.OrderType, AmountCents: order.AmountCents, PayableAmount: order.PayableAmount, Currency: order.Currency, TrafficBytes: order.TrafficBytes, EndAt: &end, RequiresPriceAcceptance: true, CreditAmount: applied, SettlementAmount: value.TotalMinor, CurrentResidual: value.CurrentMinor, FutureResidual: value.FutureMinor, SurplusAmount: value.TotalMinor - applied, AdditionalAmount: order.PayableAmount - applied}
	// Commit the price, duration and exact source amounts, not an estimated
	// start/end clock or sub-cent movement in an active flow's byte counter.
	snapshot.QuotedAt = time.Time{}
	snapshot.ValueFingerprint = ""
	quoted := preview
	quoted.EndAt = nil
	token, err := json.Marshal(struct {
		Buyer, Target, SKU uint
		Unit               string
		Period             int
		Quote              commerce.OrderPreview
		Source             changeSnapshot
	}{order.UserID, sub.ID, order.PlanSKUID, order.BillingUnit, order.BillingValue, quoted, snapshot})
	if err != nil {
		return empty, err
	}
	sum := sha256.Sum256(token)
	preview.QuoteFingerprint = hex.EncodeToString(sum[:])
	return preview, nil
}

func sameMinorSources(left, right map[uint]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for id, amount := range left {
		if right[id] != amount {
			return false
		}
	}
	return true
}
func validateFullReplacementQuote(tx *gorm.DB, order model.Order, snapshot changeSnapshot, now time.Time) error {
	var sub model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error; err != nil {
		return orderResourceError(err, "target_subscription_id", "目标订阅不存在。")
	}
	if sub.EntitlementEpoch != snapshot.EntitlementEpoch || sub.PlanID != snapshot.PlanID || sub.PlanSKUID != snapshot.PlanSKUID || !sub.EndAt.Equal(snapshot.EndAt) || sub.CycleStartUsed != snapshot.CycleStartUsed || sub.ResetQuotaBytes != snapshot.ResetQuota {
		return quoteError("原订阅已变化，请重新确认完整换购。")
	}
	check := order
	if _, err := quoteFullReplacement(tx, &check, sub, now, snapshot.QuotedAt); err != nil {
		return err
	}
	current, err := fullReplacementSnapshot(check)
	if err != nil {
		return err
	}
	if current == nil || current.QuotaEventID != snapshot.QuotaEventID || current.SourceOrderID != snapshot.SourceOrderID || current.Residual != snapshot.Residual || current.Applied != snapshot.Applied || current.Surplus != snapshot.Surplus || !sameMinorSources(current.Sources, snapshot.Sources) {
		return quoteError("原订阅的可结算金额或来源已变化，请重新确认换购报价。")
	}
	return nil
}
