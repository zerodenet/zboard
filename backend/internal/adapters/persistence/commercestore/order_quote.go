package commercestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A paid change carries the consumed credit into the new entitlement's value.
// Binding its source also prevents two pending orders from spending it twice.
type changeSnapshot struct {
	PlanID, PlanSKUID          uint
	EndAt                      time.Time
	CycleStartUsed, ResetQuota int64
	SourceOrderID              uint
	QuotaEventID               uint
	QuotedAt                   time.Time
	Credit                     int64
}

func quoteError(message string) error {
	return &commerce.ValidationError{Message: message, Fields: map[string]string{"target_subscription_id": message}}
}
func (s OrderCreation) Preview(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.OrderPreview, error) {
	var preview commerce.OrderPreview
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		_, preview, err = prepareOrder(tx, buyer, request, time.Now().UTC())
		return err
	})
	return preview, err
}
func sourceValue(tx *gorm.DB, sub model.Subscription, currency string) (int64, time.Time, uint, error) {
	var orders []model.Order
	if err := tx.Select("id", "plan_id", "paid_amount", "refund_amount", "currency", "order_type", "change_snapshot", "paid_at", "fulfilled_at", "subscription_ended_at").Where("subscription_id = ? AND status = ? AND order_type IN ?", sub.ID, "paid", []string{"new", "renewal", "upgrade"}).Order("id desc").Find(&orders).Error; err != nil {
		return 0, time.Time{}, 0, err
	}
	value := int64(0)
	start := sub.StartAt
	var root uint
	for _, order := range orders {
		if order.PlanID != sub.PlanID || order.SubscriptionEndedAt != nil {
			break
		}
		paid := max(int64(0), order.PaidAmount-order.RefundAmount)
		var snapshot changeSnapshot
		if order.OrderType == "upgrade" && order.ChangeSnapshot != "" {
			if err := json.Unmarshal([]byte(order.ChangeSnapshot), &snapshot); err != nil {
				return 0, start, 0, err
			}
		}
		credit := max(int64(0), snapshot.Credit)
		if paid > 0 || credit > 0 {
			if order.Currency != currency {
				return 0, start, 0, quoteError("不同币种的套餐不能自动抵扣。")
			}
			if value > math.MaxInt64-paid || value+paid > math.MaxInt64-credit {
				return 0, start, 0, quoteError("原订阅金额超出可计算范围。")
			}
			value += paid + credit
		}
		// A grace-period recovery starts a new paid series; terminal historical
		// orders cannot add their already-spent price to its transferable value.
		root = order.ID
		if order.PaidAt != nil {
			start = *order.PaidAt
		} else if order.FulfilledAt != nil {
			start = *order.FulfilledAt
		}
		if order.OrderType == "new" || order.OrderType == "upgrade" {
			break
		}
	}
	return value, start, root, nil
}
func applyTargetQuote(tx *gorm.DB, order *model.Order, sub model.Subscription, now, creditAt time.Time) (commerce.OrderPreview, error) {
	preview := commerce.OrderPreview{OrderType: order.OrderType, AmountCents: order.AmountCents, PayableAmount: order.PayableAmount, Currency: order.Currency, TrafficBytes: order.TrafficBytes}
	if order.OrderType == "renewal" && !entitlements.CanRenewAt(entitlements.Subscription(sub), now) {
		return preview, quoteError(entitlements.ErrRenewalWindow.Error())
	}
	if order.TargetSubscriptionID != nil && order.OrderType != "renewal" && (sub.EndedAt != nil || !sub.EndAt.After(now)) {
		return preview, quoteError("订阅已结束，请续费恢复或新购套餐。")
	}
	if order.OrderType != "upgrade" && order.OrderType != "traffic_reset" {
		return preview, nil
	}
	if !sub.EndAt.After(now) || (sub.Status != "active" && sub.Status != "expired") || entitlements.IsPerpetualEnd(sub.EndAt) {
		return preview, quoteError("请选择尚未到期的限时订阅。")
	}
	if sub.NextResetAt != nil && !sub.NextResetAt.After(now) {
		return preview, quoteError("订阅流量周期正在重置，请刷新后重试。")
	}
	snapshot := changeSnapshot{PlanID: sub.PlanID, PlanSKUID: sub.PlanSKUID, EndAt: sub.EndAt, CycleStartUsed: sub.CycleStartUsed, ResetQuota: sub.ResetQuotaBytes, QuotedAt: creditAt}
	if err := tx.Model(&model.QuotaEvent{}).Where("subscription_id = ?", sub.ID).Select("COALESCE(MAX(id), 0)").Scan(&snapshot.QuotaEventID).Error; err != nil {
		return preview, err
	}
	preview.EndAt = &sub.EndAt
	_, used := entitlements.CycleQuota(entitlements.Subscription(sub))
	preview.UsedBytes = used
	if order.OrderType == "traffic_reset" {
		if sub.ResetQuotaBytes <= 0 {
			return preview, quoteError("目标订阅没有可重置的套餐额度。")
		}
		preview.UsedBytes = 0
		order.TrafficBytes = sub.ResetQuotaBytes
		order.DeviceLimit, order.SpeedLimitMbps = 0, 0
		preview.TrafficBytes = order.TrafficBytes
	} else {
		if order.BillingUnit == "once" {
			return preview, quoteError("保留原到期日的套餐切换不能使用永久规格。")
		}
		if !entitlements.CanChangeAt(entitlements.Subscription(sub), now) {
			return preview, quoteError("目标订阅已结束或不可切换，请新购套餐。")
		}
		if order.TrafficBytes < used {
			return preview, quoteError("新套餐流量低于当前周期已用流量，请选择更大的套餐。")
		}
		value, start, root, err := sourceValue(tx, sub, order.Currency)
		if err != nil {
			return preview, err
		}
		snapshot.SourceOrderID = root
		preview.TimeCredit, preview.TrafficCredit = commerce.RemainingCredit(value, start, sub.EndAt, creditAt, sub.ResetQuotaBytes, used)
		snapshot.Credit = min(preview.TimeCredit, preview.TrafficCredit, order.AmountCents)
		order.DiscountAmount = snapshot.Credit
		order.PayableAmount = order.AmountCents - snapshot.Credit
		preview.CreditAmount, preview.PayableAmount = snapshot.Credit, order.PayableAmount
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return preview, err
	}
	order.ChangeSnapshot = string(encoded)
	// Exclude the clock from the approval token: same facts and same payable
	// amount yield the same quote until rounding or subscription facts change.
	snapshot.QuotedAt = time.Time{}
	token, err := json.Marshal(struct {
		SKU                   uint
		Buyer, Target         uint
		Unit                  string
		Value, Devices, Speed int
		Quote                 commerce.OrderPreview
		Source                changeSnapshot
	}{order.PlanSKUID, order.UserID, sub.ID, order.BillingUnit, order.BillingValue, order.DeviceLimit, order.SpeedLimitMbps, preview, snapshot})
	if err != nil {
		return preview, err
	}
	sum := sha256.Sum256(token)
	preview.QuoteFingerprint = hex.EncodeToString(sum[:])
	return preview, nil
}

func validateTargetQuote(tx *gorm.DB, order model.Order, now time.Time) error {
	if order.OrderType != "upgrade" && order.OrderType != "traffic_reset" {
		return nil
	}
	var snapshot changeSnapshot
	if order.ChangeSnapshot == "" || json.Unmarshal([]byte(order.ChangeSnapshot), &snapshot) != nil || order.TargetSubscriptionID == nil || snapshot.QuotedAt.IsZero() || snapshot.QuotedAt.After(now) {
		return quoteError("该订单缺少有效报价，请取消后重新创建。")
	}
	var sub model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error; err != nil {
		return orderResourceError(err, "target_subscription_id", "目标订阅不存在。")
	}
	if sub.PlanID != snapshot.PlanID || sub.PlanSKUID != snapshot.PlanSKUID || !sub.EndAt.Equal(snapshot.EndAt) || sub.CycleStartUsed != snapshot.CycleStartUsed || sub.ResetQuotaBytes != snapshot.ResetQuota {
		return quoteError("目标订阅已变化，请取消订单后重新确认。")
	}
	check := order
	check.ChangeSnapshot = ""
	// Lock time valuation when the order is created. Recheck present lifecycle,
	// consumption and paid/refunded source value, so waiting alone cannot reject
	// an agreed price, but a consumed or changed source cannot spend old credit.
	preview, err := applyTargetQuote(tx, &check, sub, now, snapshot.QuotedAt)
	if err != nil {
		return err
	}
	var current changeSnapshot
	if err := json.Unmarshal([]byte(check.ChangeSnapshot), &current); err != nil {
		return err
	}
	if current.QuotaEventID != snapshot.QuotaEventID {
		return quoteError("目标订阅额度已变化，请重新创建订单。")
	}
	if !sub.EndAt.After(now) || (sub.NextResetAt != nil && !sub.NextResetAt.After(now)) {
		return quoteError("订阅已到期或进入新流量周期，请重新创建订单。")
	}
	if order.OrderType == "upgrade" {
		if current.SourceOrderID != snapshot.SourceOrderID || preview.CreditAmount < snapshot.Credit {
			return quoteError("原订阅剩余权益已变化，请取消订单后重新计算抵扣。")
		}
	}
	return nil
}
