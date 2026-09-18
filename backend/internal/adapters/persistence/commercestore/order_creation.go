package commercestore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrderCreation struct{ DB *gorm.DB }

func (s OrderCreation) Create(ctx context.Context, buyer uint, request commerce.OrderCreateRequest) (commerce.Order, error) {
	var out model.Order
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = createOrderInTransaction(tx, buyer, request)
		return err
	})
	if err != nil {
		return commerce.Order{}, err
	}
	return commerce.Order(out), nil
}

// createOrderInTransaction shares snapshot persistence across owned purchases and
// administrator assignments after their caller has selected and authorized the buyer.
func createOrderInTransaction(tx *gorm.DB, buyer uint, request commerce.OrderCreateRequest) (model.Order, error) {
	if request.PlanSKUID == 0 {
		return model.Order{}, &commerce.ValidationError{Message: "订单创建失败。", Fields: map[string]string{"plan_sku_id": "请选择销售规格。"}}
	}
	var user model.User
	if buyer == 0 {
		return model.Order{}, commerce.ErrBuyerUnavailable
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&user, buyer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Order{}, commerce.ErrBuyerUnavailable
		}
		return model.Order{}, err
	}
	if user.Status != "active" {
		return model.Order{}, commerce.ErrBuyerUnavailable
	}
	// Parentage is immutable. Do not use mutable price/availability read before
	// the parent lock: an updater may commit while this transaction waits.
	var identity model.PlanSKU
	if err := tx.Select("id", "plan_id").First(&identity, request.PlanSKUID).Error; err != nil {
		return model.Order{}, orderResourceError(err, "plan_sku_id", "销售规格不存在或已停止销售。")
	}
	var plan model.Plan
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("is_active = ?", true).First(&plan, identity.PlanID).Error; err != nil {
		return model.Order{}, orderResourceError(err, "plan_sku_id", "商品不可购买。")
	}
	var sku model.PlanSKU
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND plan_id = ? AND is_active = ?", request.PlanSKUID, plan.ID, true).First(&sku).Error; err != nil {
		return model.Order{}, orderResourceError(err, "plan_sku_id", "销售规格不存在或已停止销售。")
	}
	var rows []model.PlanSKUOperation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_sku_id = ?", sku.ID).Order("operation asc").Find(&rows).Error; err != nil {
		return model.Order{}, err
	}
	operations := make([]string, 0, len(rows))
	for _, row := range rows {
		operations = append(operations, row.Operation)
	}
	view := commerce.ProjectSKU(commerce.SKURecord{SKU: commerce.SKU(sku), Operations: operations})
	var target *commerce.OrderTarget
	if request.TargetSubscriptionID != 0 {
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "plan_id").Where("id = ? AND user_id = ?", request.TargetSubscriptionID, buyer).First(&sub).Error; err != nil {
			return model.Order{}, orderResourceError(err, "target_subscription_id", "目标订阅不存在。")
		}
		target = &commerce.OrderTarget{ID: sub.ID, PlanID: sub.PlanID}
	}
	terms, err := commerce.SnapshotOrderTerms(planView(plan), commerce.SKU(sku), view.EntitlementMode, view.AllowedOperations, target, request)
	if err != nil {
		return model.Order{}, err
	}
	if terms.OrderType == "new" {
		if err := CheckSubscriptionCapacity(tx, plan, time.Now().UTC()); err != nil {
			return model.Order{}, err
		}
	}
	order := model.Order{
		UserID: buyer, PlanID: plan.ID, PlanSKUID: sku.ID,
		TradeNo: uuid.NewString(), OrderType: terms.OrderType, TargetSubscriptionID: terms.TargetSubscriptionID,
		AmountCents: terms.AmountCents, PayableAmount: terms.PayableAmount, Currency: terms.Currency,
		Channel: terms.Channel, Status: "pending",
		PlanName: terms.PlanName, SKUName: terms.SKUName, BillingUnit: terms.BillingUnit,
		BillingValue: terms.BillingValue, RenewalEffect: terms.RenewalEffect, TrafficBytes: terms.TrafficBytes,
		DeviceLimit: terms.DeviceLimit, SpeedLimitMbps: terms.SpeedLimitMbps,
	}
	if err := tx.Create(&order).Error; err != nil {
		return model.Order{}, err
	}
	return order, nil
}

func orderResourceError(err error, field, message string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &commerce.ValidationError{Message: "订单创建失败。", Fields: map[string]string{field: message}}
	}
	return err
}

// CheckSubscriptionCapacity requires the parent plan lock. Capacity remains an
// active-entitlement check; pending orders do not reserve a subscription slot.
func CheckSubscriptionCapacity(tx *gorm.DB, plan model.Plan, now time.Time) error {
	return entitlementstore.CheckCapacity(tx, plan.ID, plan.MaxActiveSubscriptions, now)
}
