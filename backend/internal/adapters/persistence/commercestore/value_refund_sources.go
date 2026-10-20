package commercestore

import (
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"time"
)

// Caller locks source orders and the subscription. Refund source budgets and
// source journal rows advance together; commercial discounts are never funding.
func refundValueSources(tx *gorm.DB, actor uint, currency string, settlementID uint, sources map[uint]int64, orders []model.Order, now time.Time) (int64, error) {
	indexed := map[uint]model.Order{}
	for _, order := range orders {
		indexed[order.ID] = order
	}
	ids := make([]uint, 0, len(sources))
	for orderID, amount := range sources {
		if amount > 0 {
			ids = append(ids, orderID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var total int64
	for _, orderID := range ids {
		order, ok := indexed[orderID]
		amount := sources[orderID]
		if !ok || order.Status != "paid" || order.Currency != currency || order.UserID != actor || amount <= 0 || amount > order.PaidAmount || order.RefundAmount != 0 {
			return 0, commerce.ErrSubscriptionValueUnavailable
		}
		budget, err := lockOrderRefundBudget(tx, order, now)
		if err != nil {
			return 0, err
		}
		if err := consumeOrderRefundBudget(tx, order, budget, amount, now); err != nil {
			return 0, err
		}
		if err := tx.Create(&model.SubscriptionValueSettlementSource{SettlementID: settlementID, OrderID: orderID, AmountMinor: amount}).Error; err != nil {
			return 0, err
		}
		var valid bool
		total, valid = addWalletMinor(total, amount)
		if !valid {
			return 0, commerce.ErrWalletRange
		}
	}
	return total, nil
}

// The caller holds the source order lock. Every outgoing consumption refund,
// replacement and unsubscribe consumes this same actual-paid budget.
func lockOrderRefundBudget(tx *gorm.DB, order model.Order, now time.Time) (model.OrderValueBudget, error) {
	var budget model.OrderValueBudget
	var fact commerce.EntitlementValueSnapshot
	if order.Status != "paid" || order.OrderType == "topup" || order.PaidAmount <= 0 || order.FulfilledAt == nil || json.Unmarshal([]byte(order.EntitlementValueSnapshot), &fact) != nil || fact.Version != 1 || !fact.PaymentVerified || fact.OrderID != order.ID || fact.SubscriptionID != order.SubscriptionID || fact.Currency != order.Currency || fact.ActualPaidMinor != order.PaidAmount {
		return budget, commerce.ErrSubscriptionValueUnavailable
	}
	basisFP := walletKey(order.EntitlementValueSnapshot)
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", order.ID).First(&budget).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Never manufacture an opening balance when the surviving order/journal
		// proves that a previous refund already consumed part of the source.
		var surviving int64
		if err := tx.Model(&model.SubscriptionValueSettlementSource{}).Where("order_id = ?", order.ID).Count(&surviving).Error; err != nil {
			return budget, err
		}
		if surviving != 0 || order.RefundAmount != 0 {
			return budget, commerce.ErrSubscriptionValueUnavailable
		}
		budget = model.OrderValueBudget{OrderID: order.ID, UserID: order.UserID, Currency: order.Currency, FundingMinor: order.PaidAmount, FundingFingerprint: basisFP, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&budget).Error; err != nil {
			return budget, err
		}
	} else if err != nil {
		return budget, err
	}
	if budget.UserID != order.UserID || budget.Currency != order.Currency || budget.FundingMinor != order.PaidAmount || budget.FundingFingerprint != basisFP || budget.RefundedMinor != order.RefundAmount || budget.RefundedMinor < 0 || budget.ConvertedMinor < 0 || budget.RefundedMinor > budget.FundingMinor || budget.ConvertedMinor > budget.FundingMinor-budget.RefundedMinor {
		return budget, commerce.ErrSubscriptionSettlementConflict
	}
	return budget, nil
}

func consumeOrderRefundBudget(tx *gorm.DB, order model.Order, budget model.OrderValueBudget, amount int64, now time.Time) error {
	if amount <= 0 || amount > budget.FundingMinor-budget.RefundedMinor-budget.ConvertedMinor {
		return commerce.ErrSubscriptionSettlementConflict
	}
	if err := tx.Model(&model.OrderValueBudget{}).Where("order_id = ?", order.ID).Updates(map[string]any{"refunded_minor": budget.RefundedMinor + amount, "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{"refund_amount": budget.RefundedMinor + amount, "refunded_at": now, "updated_at": now}).Error
}
