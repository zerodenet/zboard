package commercestore

import (
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"time"
)

func appendWalletCredit(tx *gorm.DB, order model.Order, key, kind, reason string, amount int64, now time.Time) (uint, error) {
	if amount <= 0 {
		return 0, commerce.ErrPaymentFactConflict
	}
	if currency, err := commerce.NormalizeWalletCurrency(order.Currency); err != nil || currency != order.Currency {
		return 0, commerce.ErrPaymentFactConflict
	}
	account := model.WalletAccount{UserID: order.UserID, Currency: order.Currency, Revision: 1}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return 0, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND currency = ?", order.UserID, order.Currency).First(&account).Error; err != nil {
		return 0, err
	}
	var existing model.WalletEntry
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("entry_key = ?", key).First(&existing).Error
	if err == nil {
		if existing.UserID != order.UserID || existing.Currency != order.Currency || existing.BalanceDelta != amount || existing.Kind != kind || existing.SourceOrderID == nil || *existing.SourceOrderID != order.ID {
			return 0, commerce.ErrPaymentFactConflict
		}
		return existing.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if account.BalanceMinor < 0 || account.ReservedMinor < 0 || account.ReservedMinor > account.BalanceMinor {
		return 0, commerce.ErrWalletUnavailable
	}
	if account.BalanceMinor > math.MaxInt64-amount {
		return 0, commerce.ErrWalletRange
	}
	entry := model.WalletEntry{EntryKey: key, UserID: order.UserID, Currency: order.Currency, Kind: kind, BalanceDelta: amount, BalanceBefore: account.BalanceMinor, BalanceAfter: account.BalanceMinor + amount, ReservedBefore: account.ReservedMinor, ReservedAfter: account.ReservedMinor, SourceOrderID: &order.ID, Reason: reason, CreatedAt: now}
	if err := tx.Create(&entry).Error; err != nil {
		return 0, err
	}
	err = tx.Model(&model.WalletAccount{}).Where("user_id = ? AND currency = ?", order.UserID, order.Currency).Updates(map[string]any{"balance_minor": entry.BalanceAfter, "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error
	return entry.ID, err
}
