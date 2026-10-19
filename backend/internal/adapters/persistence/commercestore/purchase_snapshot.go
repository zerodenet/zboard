package commercestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func bindPurchaseQuote(preview *commerce.OrderPreview, order model.Order) {
	if preview.QuoteFingerprint == "" {
		return
	}
	payload, _ := json.Marshal(struct {
		Original, Policy string
		Traffic          int64
		Speed, Devices   int
	}{preview.QuoteFingerprint, order.PurchaseSnapshot, order.TrafficBytes, order.SpeedLimitMbps, order.DeviceLimit})
	sum := sha256.Sum256(payload)
	preview.QuoteFingerprint = hex.EncodeToString(sum[:])
}

func capturePurchaseSnapshot(tx *gorm.DB, order *model.Order, plan model.Plan, sub model.Subscription) error {
	var renewalSKUs int64
	if err := tx.Model(&model.PlanSKUOperation{}).Joins("JOIN plan_skus ON plan_skus.id = plan_sku_operations.plan_sku_id").Where("plan_skus.plan_id = ? AND plan_skus.archived_at IS NULL AND plan_sku_operations.operation = 'renew'", plan.ID).Count(&renewalSKUs).Error; err != nil {
		return err
	}
	snapshot := commerce.PurchaseSnapshot{Version: 1, BasePriceMinor: order.AmountCents, Currency: order.Currency, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, RenewalEffect: order.RenewalEffect, Renewable: plan.IsRenewable && renewalSKUs > 0, ResetPolicy: plan.ResetPolicy, TrafficCalcMode: plan.TrafficCalcMode, FamilyLimit: plan.FamilyLimit}
	if order.OrderType == "renewal" {
		// Early renewal must not move the existing quota schedule or change the
		// metering interpretation when a catalog policy was edited meanwhile.
		snapshot.ResetPolicy, snapshot.TrafficCalcMode = sub.ResetPolicy, sub.TrafficCalcMode
		target := renewalTargetSnapshot(sub)
		snapshot.RenewalTarget = &target
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	order.PurchaseSnapshot = string(payload)
	return nil
}

func applyPurchaseSnapshot(order model.Order, policy entitlements.GrantPolicy) (entitlements.GrantPolicy, error) {
	if order.PurchaseSnapshot == "" {
		// Honor pre-upgrade pending orders with their existing compatibility
		// behavior; do not claim their missing policy was captured at purchase.
		return policy, nil
	}
	var snapshot commerce.PurchaseSnapshot
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &fields) != nil {
		return policy, invalidPurchaseSnapshot()
	}
	for _, key := range []string{"version", "base_price_minor", "currency", "billing_unit", "billing_value", "renewal_effect", "renewable", "reset_policy", "traffic_calc_mode", "family_limit"} {
		if value, exists := fields[key]; !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return policy, invalidPurchaseSnapshot()
		}
	}
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &snapshot) != nil || snapshot.Version != 1 ||
		snapshot.BasePriceMinor < 0 || snapshot.BasePriceMinor != order.AmountCents || snapshot.Currency != order.Currency ||
		snapshot.BillingUnit != order.BillingUnit || snapshot.BillingValue != order.BillingValue ||
		snapshot.RenewalEffect != order.RenewalEffect ||
		snapshot.ResetPolicy < 0 || snapshot.ResetPolicy > 5 || snapshot.TrafficCalcMode < 0 || snapshot.TrafficCalcMode > 2 || snapshot.FamilyLimit < 0 {
		return policy, invalidPurchaseSnapshot()
	}
	if raw, present := fields["renewal_target"]; present && (bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || snapshot.RenewalTarget == nil || order.OrderType != "renewal" || snapshot.RenewalTarget.EndAt.IsZero()) {
		return policy, invalidPurchaseSnapshot()
	}
	policy.IsRenewable, policy.ResetPolicy = snapshot.Renewable, snapshot.ResetPolicy
	policy.TrafficCalcMode, policy.FamilyLimit = snapshot.TrafficCalcMode, snapshot.FamilyLimit
	return policy, nil
}

func renewalTargetSnapshot(sub model.Subscription) commerce.RenewalTargetSnapshot {
	return commerce.RenewalTargetSnapshot{StartAt: sub.StartAt, EndAt: sub.EndAt, PlanSKUID: sub.PlanSKUID, ResetQuotaBytes: sub.ResetQuotaBytes, ResetPolicy: sub.ResetPolicy, TrafficCalcMode: sub.TrafficCalcMode, DeviceLimit: sub.DeviceLimit, SpeedLimitMbps: sub.SpeedLimitMbps, FamilyLimit: sub.FamilyLimit}
}
func validateRenewalTarget(tx *gorm.DB, order model.Order) error {
	if order.OrderType != "renewal" || order.PurchaseSnapshot == "" {
		return nil
	}
	var snapshot commerce.PurchaseSnapshot
	if json.Unmarshal([]byte(order.PurchaseSnapshot), &snapshot) != nil {
		return invalidPurchaseSnapshot()
	}
	// Previously confirmed legacy orders keep their original agreement. Never
	// backfill a target revision that was not actually captured at checkout.
	if snapshot.RenewalTarget == nil {
		return nil
	}
	if order.TargetSubscriptionID == nil {
		return invalidPurchaseSnapshot()
	}
	var sub model.Subscription
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&sub).Error; err != nil {
		return orderResourceError(err, "target_subscription_id", "目标订阅不存在。")
	}
	current, expected := renewalTargetSnapshot(sub), *snapshot.RenewalTarget
	if !current.StartAt.Equal(expected.StartAt) || !current.EndAt.Equal(expected.EndAt) {
		return quoteError("该订阅期限已由其他操作变更，请核对订单并重新确认续费。")
	}
	current.StartAt, current.EndAt = expected.StartAt, expected.EndAt
	if current != expected {
		return quoteError("该订阅权益已变化，请重新确认续费报价。")
	}
	return nil
}

func invalidPurchaseSnapshot() error {
	return quoteError("购买策略快照缺失或不受支持，请通过工单核实；不会按当前目录策略自动履约。")
}
