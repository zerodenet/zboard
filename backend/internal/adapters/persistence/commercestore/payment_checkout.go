package commercestore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PaymentCheckout struct{ DB *gorm.DB }

func (s PaymentCheckout) Open(ctx context.Context, actor, id uint, provider string) (out commerce.Order, permitted bool, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, actor).First(&order).Error; err != nil {
			return resourceError(err)
		}
		if err := orderReader(tx, actor, false); err != nil {
			return err
		}
		out = commerce.Order(order)
		var attempt model.PaymentAttempt
		err := tx.Where("order_id = ? AND provider = ?", id, provider).First(&attempt).Error
		if err == nil {
			if attempt.AmountMinor != order.PayableAmount || attempt.Currency != order.Currency {
				return commerce.ErrPaymentFactConflict
			}
			permitted = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Known legacy paid bindings permit idempotent reconciliation. A fresh
		// provider cannot gain payment authority merely by opening a paid order.
		bound := order.Status == "paid" && order.Channel == provider && order.ProviderTradeNo != nil
		if order.Status != "pending" && order.Status != "failed" && !bound {
			return nil
		}
		if order.PayableAmount <= 0 {
			return nil
		}
		if err := tx.Create(&model.PaymentAttempt{OrderID: id, Provider: provider, AmountMinor: order.PayableAmount, Currency: order.Currency}).Error; err != nil {
			return err
		}
		permitted = true
		return nil
	})
	return
}
