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
	return appendWalletChange(tx, order, key, kind, reason, amount, 0, now)
}
func addWalletMinor(value, delta int64) (int64, bool) {
	if value < 0 || delta == math.MinInt64 || (delta > 0 && value > math.MaxInt64-delta) || (delta < 0 && value < -delta) {
		return 0, false
	}
	return value + delta, true
}
func appendWalletChange(tx *gorm.DB, order model.Order, key, kind, reason string, balanceDelta, reservedDelta int64, now time.Time) (uint, error) {
	if balanceDelta == 0 && reservedDelta == 0 {
		return 0, commerce.ErrWalletUnavailable
	}
	if currency, err := commerce.NormalizeWalletCurrency(order.Currency); err != nil || currency != order.Currency {
		return 0, commerce.ErrPaymentFactConflict
	}
	// A missing account with a surviving ledger is lost financial evidence;
	// do not recreate a zero summary before replaying or crediting another fact.
	if _, err := walletAvailable(tx, order, false); err != nil {
		return 0, err
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
		if existing.UserID != order.UserID || existing.Currency != order.Currency || existing.BalanceDelta != balanceDelta || existing.ReservedDelta != reservedDelta || existing.Kind != kind || existing.SourceOrderID == nil || *existing.SourceOrderID != order.ID {
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

	balanceAfter, ok := addWalletMinor(account.BalanceMinor, balanceDelta)
	if !ok {
		return 0, commerce.ErrWalletRange
	}
	reservedAfter, ok := addWalletMinor(account.ReservedMinor, reservedDelta)
	if !ok || reservedAfter > balanceAfter {
		return 0, commerce.ErrWalletUnavailable
	}
	entry := model.WalletEntry{EntryKey: key, UserID: order.UserID, Currency: order.Currency, Kind: kind, BalanceDelta: balanceDelta, ReservedDelta: reservedDelta, BalanceBefore: account.BalanceMinor, BalanceAfter: balanceAfter, ReservedBefore: account.ReservedMinor, ReservedAfter: reservedAfter, SourceOrderID: &order.ID, Reason: reason, CreatedAt: now}
	if err := tx.Create(&entry).Error; err != nil {
		return 0, err
	}
	err = tx.Model(&model.WalletAccount{}).Where("user_id = ? AND currency = ?", order.UserID, order.Currency).Updates(map[string]any{"balance_minor": entry.BalanceAfter, "reserved_minor": entry.ReservedAfter, "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error
	return entry.ID, err
}
