package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/businessref"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Caller owns the order-creation transaction. No provisional number can escape
// commit, checkout, notifications or the returned order. Existing trade numbers
// are never rewritten; they can already be bound to a payment provider.
func createNumberedOrder(tx *gorm.DB, row *model.Order) error {
	seed, e := businessref.Provisional('O')
	if e != nil {
		return e
	}
	row.TradeNo = seed
	if e := tx.Create(row).Error; e != nil {
		return e
	}
	final := businessref.Order(row.ID, row.CreatedAt)
	q := tx.Model(&model.Order{}).Where("id=? AND trade_no=?", row.ID, seed).UpdateColumn("trade_no", final)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return errors.New("order number assignment lost ownership")
	}
	row.TradeNo = final
	return nil
}
