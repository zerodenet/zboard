package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (s OrderSettlement) setPaid(tx *gorm.DB, order *model.Order, now time.Time) error {
	if order.Status == "paid" {
		return nil
	}
	if order.SubscriptionEndedAt != nil && order.TargetSubscriptionID == nil && order.OrderType != "new" {
		return quoteError(entitlements.ErrRenewalWindow.Error())
	}
	var plan model.Plan
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, order.PlanID).Error; err != nil {
		return err
	}
	var sku model.PlanSKU
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, order.PlanSKUID).Error; err != nil {
		return err
	}
	if err := validateTargetQuote(tx, *order, now); err != nil {
		return err
	}
	request := entitlements.GrantRequest{ID: order.ID, TargetSubscriptionID: order.TargetSubscriptionID, UserID: order.UserID, PlanID: order.PlanID, PlanSKUID: order.PlanSKUID, OrderType: order.OrderType, BillingUnit: order.BillingUnit, BillingValue: order.BillingValue, RenewalEffect: order.RenewalEffect, TrafficBytes: order.TrafficBytes, SpeedLimitMbps: order.SpeedLimitMbps, DeviceLimit: order.DeviceLimit}
	var renewalSKUs int64
	if err := tx.Model(&model.PlanSKUOperation{}).Joins("JOIN plan_skus ON plan_skus.id = plan_sku_operations.plan_sku_id").Where("plan_skus.plan_id = ? AND plan_skus.archived_at IS NULL AND plan_sku_operations.operation = 'renew'", plan.ID).Count(&renewalSKUs).Error; err != nil {
		return err
	}
	policy := entitlements.GrantPolicy{NodeGroupID: plan.NodeGroupID, IsRenewable: plan.IsRenewable && renewalSKUs > 0, RenewalPriceMinor: sku.PriceCents, FamilyLimit: plan.FamilyLimit, ResetPolicy: plan.ResetPolicy, TrafficCalcMode: plan.TrafficCalcMode, MaxActiveSubscriptions: plan.MaxActiveSubscriptions}
	sub, err := entitlementstore.Fulfill(tx, request, policy, s.Issuer, now)
	if err != nil {
		if errors.Is(err, entitlements.ErrRenewalWindow) {
			return quoteError(err.Error())
		}
		return err
	}
	if err := tx.Model(order).Updates(map[string]interface{}{"status": "paid", "subscription_id": sub.ID, "paid_amount": order.PayableAmount, "paid_at": now, "fulfilled_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	order.Status = "paid"
	order.SubscriptionID = sub.ID
	order.PaidAmount = order.PayableAmount
	order.PaidAt = &now
	order.FulfilledAt = &now
	order.UpdatedAt = now
	return nil
}
