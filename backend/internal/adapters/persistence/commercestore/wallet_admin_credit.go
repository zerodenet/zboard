package commercestore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s WalletAdjustments) Credit(ctx context.Context, actor uint, in commerce.WalletCreditRequest) (out commerce.WalletEntry, err error) {
	return s.changeBalance(ctx, actor, in, false)
}

func (s WalletAdjustments) DirectDebit(ctx context.Context, actor uint, in commerce.WalletBalanceRequest) (commerce.WalletEntry, error) {
	return s.changeBalance(ctx, actor, in, true)
}

func (s WalletAdjustments) changeBalance(ctx context.Context, actor uint, in commerce.WalletBalanceRequest, debit bool) (out commerce.WalletEntry, err error) {
	kind, action, delta, reason := "admin_credit", "credit", in.AmountMinor, "管理员增加余额："+in.Reason
	if debit {
		kind, action, delta, reason = "admin_debit", "debit", -in.AmountMinor, "管理员扣减余额："+in.Reason
	}
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return out, &commerce.ValidationError{Message: "请求标识无效。"}
	}
	key := walletKey(fmt.Sprintf("admin-%s:%d:%s", action, actor, in.IdempotencyKey))
	match := func(row model.WalletEntry) error {
		if row.UserID != in.UserID || row.Currency != in.Currency || row.Kind != kind || row.BalanceDelta != delta || row.ReservedDelta != 0 || row.SourceOrderID != nil || row.Reason != reason {
			return commerce.ErrWalletAdjustmentConflict
		}
		out = commerce.WalletEntry{ID: row.ID, Kind: row.Kind, BalanceDelta: row.BalanceDelta, ReservedDelta: row.ReservedDelta, BalanceAfter: row.BalanceAfter, ReservedAfter: row.ReservedAfter, Reason: row.Reason, CreatedAt: row.CreatedAt}
		return nil
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, true); err != nil {
			return err
		}
		var prior model.WalletEntry
		e := tx.Where("entry_key = ?", key).First(&prior).Error
		if e == nil {
			return match(prior)
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		// A fresh balance change cannot recreate a deleted user. The shared user lock
		// also serializes it with hard deletion while leaving disabled users manageable.
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").First(&user, in.UserID).Error; err != nil {
			return resourceError(err)
		}
		account, err := lockWalletAccount(tx, in.UserID, in.Currency)
		if err != nil {
			return err
		}
		// A current read after the account lock sees a concurrent identical
		// request even under MySQL's repeatable-read snapshot.
		e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("entry_key = ?", key).First(&prior).Error
		if e == nil {
			return match(prior)
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if debit && account.BalanceMinor-account.ReservedMinor < in.AmountMinor {
			return &commerce.ValidationError{Message: "可用余额不足，不能扣减已预留的资金。"}
		}
		now := time.Now().UTC()
		id, err := appendWalletEntry(tx, walletEntrySource{UserID: in.UserID, Currency: in.Currency}, key, kind, reason, delta, 0, now)
		if err != nil {
			return err
		}
		if err := tx.First(&prior, id).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("admin:%d", actor), Action: "wallet.adjustment." + action, Target: fmt.Sprintf("wallet-entry:%d", id), Detail: fmt.Sprintf("user:%d amount:%d currency:%s reason:%s", in.UserID, in.AmountMinor, in.Currency, in.Reason)}).Error; err != nil {
			return err
		}
		return match(prior)
	})
	if uniqueViolation(err) || errors.Is(err, commerce.ErrPaymentFactConflict) {
		err = commerce.ErrWalletAdjustmentConflict
	}
	return
}
