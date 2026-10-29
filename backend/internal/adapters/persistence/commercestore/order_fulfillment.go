package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (s OrderSettlement) setPaid(tx *gorm.DB, order *model.Order, now time.Time) error {
	return s.setPaidWithFunding(tx, order, now, false)
}
func (s OrderSettlement) setPaidWithFunding(tx *gorm.DB, order *model.Order, now time.Time, fundingConfirmed bool) error {
	if order.OrderType == "topup" {
		return quoteError("余额充值只能由已核验的支付渠道事实入账。")
	}
	if order.Status == "paid" {
		return nil
	}
	if err := RequireOrderDiscountReservation(tx, *order, now); err != nil {
		return err
	}
	if order.OrderType != "new" && order.TargetSubscriptionID == nil {
		return quoteError(entitlements.ErrGrantTargetRequired.Error())
	}
	full, err := fullReplacementSnapshot(*order)
	if err != nil {
		return err
	}
	if full != nil {
		if err := configureFullReplacementFundingIfMissing(tx, *order, now); err != nil {
			return err
		}
	}
	fund, configured, err := findOrderFunding(tx, *order)
	if err != nil {
		return err
	}
	if configured {
		if fund.State != "reserved" || !fund.ExpiresAt.After(now) {
			return commerce.ErrFundingConflict
		}
		if fund.WalletMinor > 0 && !fundingConfirmed {
			return quoteError("该订单已预留余额，只能通过余额付款或核验渠道补差完成。")
		}
	}
	if order.SubscriptionEndedAt != nil && order.TargetSubscriptionID == nil && order.OrderType != "new" {
		return quoteError(entitlements.ErrRenewalWindow.Error())
	}
	if order.SubscriptionEndedAt != nil && order.TargetSubscriptionID != nil {
		var target model.Subscription
		if err := tx.Select("id", "status").Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&target).Error; err != nil {
			return quoteError(entitlements.ErrRenewalWindow.Error())
		}
		if target.Status == "ended" || target.Status == "canceled" {
			return quoteError(entitlements.ErrRenewalWindow.Error())
		}
	}
	var plan model.Plan
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, order.PlanID).Error; err != nil {
		return err
	}
	var sku model.PlanSKU
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, order.PlanSKUID).Error; err != nil {
		return err
	}
	var recoveryGrace *time.Duration
	if order.OrderType == "renewal" {
		grace, err := entitlementstore.RecoveryGracePeriod(tx)
		if err != nil {
			return err
		}
		recoveryGrace = &grace
		var locked model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ? AND user_id = ?", *order.TargetSubscriptionID, order.UserID).First(&locked).Error; err != nil {
			return orderResourceError(err, "target_subscription_id", "目标订阅不存在。")
		}
	}
	// Locks or configuration changes may have delayed this decision. Evaluate
	// eligibility and reservation expiry with a fresh trusted clock after them.
	now = commerceNow(s.Now)
	if err := RequireOrderDiscountReservation(tx, *order, now); err != nil {
		return err
	}
	if configured && fund.State == "reserved" && !fund.ExpiresAt.After(now) {
		return quoteError("付款资金预留已失效，请核对余额并重新确认订单。")
	}
	if err := validateTargetQuote(tx, *order, now); err != nil {
		return err
	}
	request := entitlements.GrantRequest{ID: order.ID, TargetSubscriptionID: order.TargetSubscriptionID, UserID: order.UserID, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, OrderType: order.OrderType, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, RenewalEffect: order.RenewalEffect, TrafficBytes: order.TrafficBytes, SpeedLimitMbps: order.SpeedLimitMbps, DeviceLimit: order.DeviceLimit}
	request.ReplaceTerm = full != nil
	var renewalSKUs int64
	if err := tx.Model(&model.PlanSKUOperation{}).Joins("JOIN plan_skus ON plan_skus.id = plan_sku_operations.plan_sku_id").Where("plan_skus.plan_id = ? AND plan_skus.archived_at IS NULL AND plan_sku_operations.operation = 'renew'", plan.ID).Count(&renewalSKUs).Error; err != nil {
		return err
	}
	// The order's base price predates discounts and is immutable after checkout.
	// A catalog edit while payment is pending cannot change its renewal agreement.
	renewalPrice := order.AmountCents
	if order.OrderType == "renewal" && order.TargetSubscriptionID != nil {
		source, purchased, err := renewalPriceAgreement(tx, *order, *order.TargetSubscriptionID)
		if err == nil && purchased {
			renewalPrice = source.AmountCents
		} else if err != nil {
			// Already-created legacy orders keep their agreed payment/fulfillment
			// terms. Missing historical snapshots block new automatic quotes, not
			// settlement of these existing commercial commitments.
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) {
				return err
			}
		}
	}
	policy := entitlements.GrantPolicy{RecoveryGracePeriod: recoveryGrace, NodeGroupID: plan.NodeGroupID, IsRenewable: plan.IsRenewable && renewalSKUs > 0, RenewalPriceMinor: renewalPrice, FamilyLimit: plan.FamilyLimit, ResetPolicy: plan.ResetPolicy, TrafficCalcMode: plan.TrafficCalcMode, MaxActiveSubscriptions: plan.MaxActiveSubscriptions}
	policy, err = applyPurchaseSnapshot(*order, policy)
	if err != nil {
		return err
	}
	if err := validateRenewalTarget(tx, *order); err != nil {
		return err
	}
	if full != nil {
		if err := settleFullReplacement(tx, *order, *full, now); err != nil {
			return err
		}
	}
	sub, previous, err := entitlementstore.FulfillWithPrevious(tx, request, policy, s.Issuer, now)
	if err != nil {
		if errors.Is(err, entitlements.ErrRenewalWindow) || errors.Is(err, entitlements.ErrGrantTargetRequired) {
			return quoteError(err.Error())
		}
		return err
	}
	if err := captureOrderFunding(tx, *order, now); err != nil {
		return err
	}
	if err := captureEntitlementValue(tx, order, sub, previous, fundingConfirmed, now); err != nil {
		return err
	}
	return markOrderPaid(tx, order, sub.ID, now)
}

func (s OrderSettlement) setVerifiedPaid(tx *gorm.DB, order *model.Order, now time.Time) error {
	if order.OrderType != "topup" {
		return s.setPaidWithFunding(tx, order, now, true)
	}
	if order.Status == "paid" {
		return nil
	}
	if err := creditTopUp(tx, *order, now); err != nil {
		return err
	}
	return markOrderPaid(tx, order, 0, now)
}

func markOrderPaid(tx *gorm.DB, order *model.Order, subscriptionID uint, now time.Time) error {
	if err := CommitOrderDiscountRedemptionIntent(tx, *order, now); err != nil {
		return err
	}
	var subscriptionValue any
	if subscriptionID != 0 {
		subscriptionValue = subscriptionID
	}
	if err := tx.Model(order).Updates(map[string]interface{}{"status": "paid", "subscription_id": subscriptionValue, "paid_amount": order.PayableAmount, "paid_at": now, "fulfilled_at": now, "updated_at": now,
		"subscription_ended_at": nil, "subscription_end_reason": "", "subscription_final_flow_total": 0, "subscription_final_flow_used": 0}).Error; err != nil {
		return err
	}
	order.Status = "paid"
	order.SubscriptionID = subscriptionID
	order.PaidAmount = order.PayableAmount
	order.PaidAt = &now
	order.FulfilledAt = &now
	order.UpdatedAt = now
	order.SubscriptionEndedAt, order.SubscriptionEndReason = nil, ""
	order.SubscriptionFinalFlowTotal, order.SubscriptionFinalFlowUsed = 0, 0
	return nil
}
