package commercestore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
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
		fund, configured, err := findOrderFunding(tx, order)
		if err != nil {
			return err
		}
		cash := order.PayableAmount
		if configured {
			cash = fund.CashMinor
		}
		out.PayableAmount = cash
		// Existing exact provider/amount bindings retain trusted reconciliation
		// authority even after the new reservation gate closes. Mark the checkout
		// context canceled so it cannot offer another collection/prepare operation.
		var attempt model.PaymentAttempt
		err = tx.Where("order_id = ? AND provider = ?", id, provider).First(&attempt).Error
		if err == nil {
			if attempt.AmountMinor != cash || attempt.Currency != order.Currency {
				return commerce.ErrPaymentFactConflict
			}
			if order.Status == "pending" || order.Status == "failed" {
				if configured && (fund.State != "reserved" || !fund.ExpiresAt.After(time.Now().UTC())) {
					out.Status = "canceled"
				}
				if e := RequireOrderDiscountReservation(tx, order, time.Now().UTC()); e != nil {
					out.Status = "canceled"
				}
			}
			permitted = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.Status == "pending" || order.Status == "failed" {
			if err := RequireOrderDiscountReservation(tx, order, time.Now().UTC()); err != nil {
				return err
			}
			if err := configureFullReplacementFundingIfMissing(tx, order, time.Now().UTC()); err != nil {
				return err
			}
			fund, configured, err = findOrderFunding(tx, order)
			if err != nil {
				return err
			}
		}
		canPrepare := true
		if configured {
			cash = fund.CashMinor
			canPrepare = fund.State == "reserved" && fund.ExpiresAt.After(time.Now().UTC())
			if !canPrepare && (order.Status == "pending" || order.Status == "failed") {
				out.Status = "canceled"
			}
		}
		out.PayableAmount = cash
		bound := order.Status == "paid" && order.Channel == provider && order.ProviderTradeNo != nil
		if order.Status != "pending" && order.Status != "failed" && !bound {
			return nil
		}
		if cash <= 0 || (!canPrepare && !bound) {
			return nil
		}
		if err := tx.Create(&model.PaymentAttempt{OrderID: id, Provider: provider, AmountMinor: cash, Currency: order.Currency}).Error; err != nil {
			return err
		}
		permitted = true
		return nil
	})
	return
}
