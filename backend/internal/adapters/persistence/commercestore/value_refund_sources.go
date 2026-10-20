package commercestore

import (
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
		basisFP := walletKey(order.EntitlementValueSnapshot)
		var budget model.OrderValueBudget
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", orderID).First(&budget).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var surviving int64
			if err := tx.Model(&model.SubscriptionValueSettlementSource{}).Where("order_id = ?", orderID).Count(&surviving).Error; err != nil {
				return 0, err
			}
			if surviving != 0 {
				return 0, commerce.ErrSubscriptionValueUnavailable
			}
			budget = model.OrderValueBudget{OrderID: orderID, UserID: actor, Currency: currency, FundingMinor: order.PaidAmount, FundingFingerprint: basisFP, Revision: 1, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&budget).Error; err != nil {
				return 0, err
			}
		} else if e != nil {
			return 0, e
		}
		if budget.UserID != actor || budget.Currency != currency || budget.FundingMinor != order.PaidAmount || budget.FundingFingerprint != basisFP || budget.RefundedMinor != order.RefundAmount || budget.RefundedMinor < 0 || budget.ConvertedMinor < 0 || budget.RefundedMinor > budget.FundingMinor || budget.ConvertedMinor > budget.FundingMinor-budget.RefundedMinor || amount > budget.FundingMinor-budget.RefundedMinor-budget.ConvertedMinor {
			return 0, commerce.ErrSubscriptionSettlementConflict
		}
		if err := tx.Model(&model.OrderValueBudget{}).Where("order_id = ?", orderID).Updates(map[string]any{"refunded_minor": budget.RefundedMinor + amount, "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error; err != nil {
			return 0, err
		}
		if err := tx.Create(&model.SubscriptionValueSettlementSource{SettlementID: settlementID, OrderID: orderID, AmountMinor: amount}).Error; err != nil {
			return 0, err
		}
		if err := tx.Model(&model.Order{}).Where("id = ?", orderID).Updates(map[string]any{"refund_amount": amount, "updated_at": now}).Error; err != nil {
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
