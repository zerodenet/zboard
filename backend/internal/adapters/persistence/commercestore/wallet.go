package commercestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Wallet struct{ DB *gorm.DB }

func walletKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s Wallet) Read(ctx context.Context, actor uint, currency string, after uint, limit int) (out commerce.WalletPage, err error) {
	out = commerce.WalletPage{Currency: currency, Entries: []commerce.WalletEntry{}}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, false); err != nil {
			return err
		}
		var account model.WalletAccount
		err := tx.Where("user_id = ? AND currency = ?", actor, currency).First(&account).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if account.BalanceMinor < 0 || account.ReservedMinor < 0 || account.ReservedMinor > account.BalanceMinor {
			return commerce.ErrWalletUnavailable
		}
		out.BalanceMinor, out.ReservedMinor, out.AvailableMinor = account.BalanceMinor, account.ReservedMinor, account.BalanceMinor-account.ReservedMinor
		var entries []model.WalletEntry
		query := tx.Where("user_id = ? AND currency = ?", actor, currency)
		if after != 0 {
			query = query.Where("id < ?", after)
		}
		if err := query.Order("id desc").Limit(limit + 1).Find(&entries).Error; err != nil {
			return err
		}
		if account.UserID == 0 && len(entries) > 0 {
			return commerce.ErrWalletUnavailable
		}
		out.HasMore = len(entries) > limit
		if out.HasMore {
			entries = entries[:limit]
		}
		for _, row := range entries {
			out.Entries = append(out.Entries, commerce.WalletEntry{ID: row.ID, Kind: row.Kind, BalanceDelta: row.BalanceDelta, ReservedDelta: row.ReservedDelta, BalanceAfter: row.BalanceAfter, ReservedAfter: row.ReservedAfter, SourceOrderID: row.SourceOrderID, Reason: row.Reason, CreatedAt: row.CreatedAt})
		}
		return nil
	})
	return
}
func (s Wallet) CreateTopUp(ctx context.Context, actor uint, in commerce.TopUpRequest) (commerce.Order, error) {
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return commerce.Order{}, &commerce.ValidationError{Message: "充值请求标识无效。"}
	}
	key := walletKey(fmt.Sprintf("wallet-topup:%d:%s", actor, in.IdempotencyKey))
	payload, _ := json.Marshal(in)
	fingerprint := walletKey(string(payload))
	var order model.Order
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "email", "status").First(&user, actor).Error; err != nil {
			return resourceError(err)
		}
		if user.Status != "active" {
			return commerce.ErrOrderPermission
		}
		err := tx.Where("creation_key = ? AND user_id = ?", key, actor).First(&order).Error
		if err == nil {
			if order.CreationFingerprint != fingerprint {
				return &commerce.ValidationError{Message: "同一充值标识不能更改金额或币种。"}
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		order = model.Order{UserID: actor, CreationKey: &key, CreationFingerprint: fingerprint, TradeNo: uuid.NewString(), OrderType: "topup", AmountCents: in.AmountMinor, PayableAmount: in.AmountMinor, Currency: in.Currency, Channel: "manual", Status: "pending", PlanName: "余额充值", SKUName: "充值", BillingUnit: "once", BillingValue: 1, RenewalEffect: "none"}
		// Top-ups have no plan, SKU or entitlement. Never create dummy sale rows.
		if err := tx.Omit("PlanID", "PlanSKUID", "SubscriptionID", "TargetSubscriptionID").Create(&order).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &actor, Actor: user.Email, Action: "wallet.topup.create", Target: fmt.Sprintf("order:%d", order.ID), Detail: fmt.Sprintf("amount_minor=%d currency=%s", in.AmountMinor, in.Currency)}).Error
	})
	if err != nil {
		return commerce.Order{}, err
	}
	return commerce.Order(order), nil
}

// Called only by the trusted payment-fact transaction, after order and account
// authorization. Source order identity makes a repeated provider event or a
// provider's new event ID for the same receipt unable to credit twice.
func creditTopUp(tx *gorm.DB, order model.Order, now time.Time) error {
	if order.OrderType != "topup" || order.PayableAmount <= 0 || order.AmountCents != order.PayableAmount || order.DiscountAmount != 0 {
		return commerce.ErrPaymentFactConflict
	}
	if _, err := commerce.NormalizeWalletCurrency(order.Currency); err != nil {
		return err
	}
	key := walletKey("topup-order:" + strconv.FormatUint(uint64(order.ID), 10))
	account := model.WalletAccount{UserID: order.UserID, Currency: order.Currency, Revision: 1}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND currency = ?", order.UserID, order.Currency).First(&account).Error; err != nil {
		return err
	}
	var existing model.WalletEntry
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("entry_key = ?", key).First(&existing).Error
	if err == nil {
		if existing.UserID != order.UserID || existing.Currency != order.Currency || existing.BalanceDelta != order.PayableAmount || existing.Kind != "topup" {
			return commerce.ErrPaymentFactConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if account.BalanceMinor < 0 || account.ReservedMinor < 0 || account.ReservedMinor > account.BalanceMinor {
		return commerce.ErrWalletUnavailable
	}
	if account.BalanceMinor > math.MaxInt64-order.PayableAmount {
		return commerce.ErrWalletRange
	}
	entry := model.WalletEntry{EntryKey: key, UserID: order.UserID, Currency: order.Currency, Kind: "topup", BalanceDelta: order.PayableAmount, BalanceBefore: account.BalanceMinor, BalanceAfter: account.BalanceMinor + order.PayableAmount, ReservedBefore: account.ReservedMinor, ReservedAfter: account.ReservedMinor, SourceOrderID: &order.ID, Reason: "已核验的渠道充值入账", CreatedAt: now}
	if err := tx.Create(&entry).Error; err != nil {
		return err
	}
	return tx.Model(&model.WalletAccount{}).Where("user_id = ? AND currency = ?", order.UserID, order.Currency).Updates(map[string]any{"balance_minor": entry.BalanceAfter, "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error
}
