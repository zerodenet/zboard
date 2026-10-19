package commercestore

import (
	"context"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

const orderFundingLifetime = 30 * time.Minute

type WalletPayments struct {
	Now    func() time.Time
	DB     *gorm.DB
	Issuer entitlements.CredentialIssuer
}

func findOrderFunding(tx *gorm.DB, order model.Order) (fund model.OrderFunding, exists bool, err error) {
	err = tx.Where("order_id = ?", order.ID).First(&fund).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fund, false, nil
	}
	if err != nil {
		return fund, false, err
	}
	if fund.UserID != order.UserID || fund.Currency != order.Currency || fund.AmountMinor != order.PayableAmount || fund.AmountMinor < 0 || fund.WalletMinor < 0 || fund.CashMinor < 0 || fund.WalletMinor > fund.AmountMinor || fund.AmountMinor-fund.WalletMinor != fund.CashMinor {
		return fund, false, commerce.ErrWalletUnavailable
	}
	return fund, true, nil
}
func walletAvailable(tx *gorm.DB, userID uint, currency string, lock bool) (int64, error) {
	var account model.WalletAccount
	query := tx.Where("user_id = ? AND currency = ?", userID, currency)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var present struct{ Found int }
			if err := tx.Model(&model.WalletEntry{}).Where("user_id = ? AND currency = ?", userID, currency).Select("1 AS found").Limit(1).Scan(&present).Error; err != nil {
				return 0, err
			}
			if present.Found == 1 {
				return 0, commerce.ErrWalletUnavailable
			}
			return 0, nil
		}
		return 0, err
	}
	if account.BalanceMinor < 0 || account.ReservedMinor < 0 || account.ReservedMinor > account.BalanceMinor {
		return 0, commerce.ErrWalletUnavailable
	}
	return account.BalanceMinor - account.ReservedMinor, nil
}
func fundingView(tx *gorm.DB, order model.Order, now time.Time) (out commerce.OrderFundingView, err error) {
	available, err := walletAvailable(tx, order.UserID, order.Currency, false)
	if err != nil {
		return out, err
	}
	out = commerce.OrderFundingView{OrderID: order.ID, Currency: order.Currency, AmountMinor: order.PayableAmount, CashMinor: order.PayableAmount, AvailableMinor: available, State: "unconfigured"}
	fund, exists, err := findOrderFunding(tx, order)
	if err != nil {
		return out, err
	}
	if exists {
		out.Configured = true
		out.UseBalance = fund.UseBalance
		out.WalletMinor = fund.WalletMinor
		out.CashMinor = fund.CashMinor
		out.State = fund.State
		out.ExpiresAt = &fund.ExpiresAt
		out.Expired = fund.State == "reserved" && !fund.ExpiresAt.After(now)
		out.ReleaseReason = fund.ReleaseReason
	}
	return out, nil
}
func (s WalletPayments) Read(ctx context.Context, actor, id uint) (out commerce.OrderFundingView, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, actor, false); err != nil {
			return err
		}
		var order model.Order
		if err := tx.Where("id = ? AND user_id = ?", id, actor).First(&order).Error; err != nil {
			return resourceError(err)
		}
		var err error
		out, err = fundingView(tx, order, commerceNow(s.Now))
		return err
	})
	return
}
func configureOrderFunding(tx *gorm.DB, order model.Order, in commerce.FundingRequest, now time.Time) error {
	if order.OrderType == "topup" {
		return quoteError("充值不能使用余额付款或预留，请选择支付渠道。")
	}
	if _, err := commerce.NormalizeWalletCurrency(order.Currency); err != nil {
		return err
	}
	fund, exists, err := findOrderFunding(tx, order)
	if err != nil {
		return err
	}
	if exists {
		if fund.UseBalance != in.UseBalance {
			return commerce.ErrFundingConflict
		}
		return nil
	}
	if order.Status != "pending" && order.Status != "failed" {
		return commerce.ErrFundingConflict
	}
	if order.PayableAmount < 0 {
		return commerce.ErrWalletRange
	}
	available, err := walletAvailable(tx, order.UserID, order.Currency, true)
	if err != nil {
		return err
	}
	wallet := int64(0)
	if in.UseBalance {
		wallet = min(available, order.PayableAmount)
	}
	if wallet > 0 {
		var attempts, events int64
		if err := tx.Model(&model.PaymentAttempt{}).Where("order_id = ?", order.ID).Count(&attempts).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PaymentEvent{}).Where("order_id = ?", order.ID).Count(&events).Error; err != nil {
			return err
		}
		if attempts > 0 || events > 0 || order.ProviderTradeNo != nil {
			return quoteError("此订单已发起渠道付款，不能改变补差金额。请先核对或取消，再创建新订单使用余额。")
		}
		if _, err := appendWalletChange(tx, order, walletKey("reserve-order:"+strconv.FormatUint(uint64(order.ID), 10)), "reserve", "订单付款预留余额", 0, wallet, now); err != nil {
			return err
		}
	}
	fund = model.OrderFunding{OrderID: order.ID, UserID: order.UserID, Currency: order.Currency, UseBalance: in.UseBalance, AmountMinor: order.PayableAmount, WalletMinor: wallet, CashMinor: order.PayableAmount - wallet, State: "reserved", ExpiresAt: now.Add(orderFundingLifetime), CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&fund).Error; err != nil {
		return err
	}
	actor := order.UserID
	return tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("user:%d", actor), Action: "commerce.funding.reserve", Target: fmt.Sprintf("order:%d", order.ID), Detail: fmt.Sprintf("wallet:%d cash:%d currency:%s", fund.WalletMinor, fund.CashMinor, fund.Currency)}).Error
}
func (s WalletPayments) Configure(ctx context.Context, actor, id uint, in commerce.FundingRequest) (out commerce.OrderFundingView, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, actor).First(&order).Error; err != nil {
			return resourceError(err)
		}
		if err := orderReader(tx, actor, false); err != nil {
			return err
		}
		now := commerceNow(s.Now)
		if err := configureOrderFunding(tx, order, in, now); err != nil {
			return err
		}
		var err error
		out, err = fundingView(tx, order, now)
		return err
	})
	return
}
func releaseOrderFunding(tx *gorm.DB, order model.Order, reason string, now time.Time) error {
	fund, exists, err := findOrderFunding(tx, order)
	if err != nil || !exists {
		return err
	}
	if fund.State == "released" || fund.State == "captured" {
		return nil
	}
	if fund.State != "reserved" {
		return commerce.ErrWalletUnavailable
	}
	if fund.WalletMinor > 0 {
		if _, err := appendWalletChange(tx, order, walletKey("release-order:"+strconv.FormatUint(uint64(order.ID), 10)), "reserve_release", "订单取消、失败或失效释放余额", 0, -fund.WalletMinor, now); err != nil {
			return err
		}
	}
	return tx.Model(&fund).Updates(map[string]any{"state": "released", "released_at": now, "release_reason": reason, "updated_at": now}).Error
}
func captureOrderFunding(tx *gorm.DB, order model.Order, now time.Time) error {
	fund, exists, err := findOrderFunding(tx, order)
	if err != nil || !exists {
		return err
	}
	if fund.State != "reserved" || !fund.ExpiresAt.After(now) {
		return commerce.ErrFundingConflict
	}
	if fund.WalletMinor > 0 {
		if _, err := appendWalletChange(tx, order, walletKey("capture-order:"+strconv.FormatUint(uint64(order.ID), 10)), "purchase", "订单余额付款", -fund.WalletMinor, -fund.WalletMinor, now); err != nil {
			return err
		}
	}
	return tx.Model(&fund).Updates(map[string]any{"state": "captured", "captured_at": now, "updated_at": now}).Error
}
func (s WalletPayments) Pay(ctx context.Context, actor, id uint) (out commerce.WalletPaymentResult, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, actor).First(&order).Error; err != nil {
			return resourceError(err)
		}
		if err := orderReader(tx, actor, false); err != nil {
			return err
		}
		fund, exists, err := findOrderFunding(tx, order)
		if err != nil {
			return err
		}
		if !exists || order.OrderType == "topup" || fund.CashMinor != 0 {
			return quoteError("此订单尚未由余额覆盖，请先确认付款分配或完成渠道补差。")
		}
		out = commerce.WalletPaymentResult{OrderID: order.ID, SubscriptionID: order.SubscriptionID, Status: order.Status}
		if order.Status == "paid" && fund.State == "captured" && order.Channel == "wallet" {
			return nil
		}
		now := commerceNow(s.Now)
		if (order.Status != "pending" && order.Status != "failed") || fund.State != "reserved" || !fund.ExpiresAt.After(now) {
			return quoteError("余额预留已失效，请核对余额后重新创建订单。")
		}
		settlement := OrderSettlement{DB: s.DB, Issuer: s.Issuer}
		if err := settlement.setPaidWithFunding(tx, &order, now, true); err != nil {
			return err
		}
		if err := tx.Model(&order).Update("channel", "wallet").Error; err != nil {
			return err
		}
		if order.SubscriptionID != 0 {
			if err := networkstore.EnqueueSubscriptionPublications(tx, order.SubscriptionID, actor); err != nil {
				return err
			}
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: fmt.Sprintf("user:%d", actor), Action: "commerce.wallet.pay", Target: fmt.Sprintf("order:%d", id), Detail: fmt.Sprintf("amount_minor:%d currency:%s", order.PayableAmount, order.Currency)}).Error; err != nil {
			return err
		}
		out = commerce.WalletPaymentResult{OrderID: id, SubscriptionID: order.SubscriptionID, Status: order.Status, Fulfilled: true}
		return nil
	})
	if errors.Is(err, entitlements.ErrCapacity) {
		err = commerce.ErrSubscriptionCapacity
	}
	return
}
func (s WalletPayments) Expire(ctx context.Context, now time.Time, limit int) (processed int, err error) {
	var ids []uint
	if err := s.DB.WithContext(ctx).Model(&model.OrderFunding{}).Where("state = ? AND expires_at <= ?", "reserved", now).Order("expires_at asc, order_id asc").Limit(limit).Pluck("order_id", &ids).Error; err != nil {
		return 0, err
	}
	for _, id := range ids {
		changed := false
		err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var order model.Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, id).Error; err != nil {
				return err
			}
			fund, exists, err := findOrderFunding(tx, order)
			if err != nil {
				return err
			}
			if !exists || fund.State != "reserved" || fund.ExpiresAt.After(now) {
				return nil
			}
			if order.Status != "pending" && order.Status != "failed" && order.Status != "canceled" {
				return commerce.ErrWalletUnavailable
			}
			if err := releaseOrderFunding(tx, order, "expired", now); err != nil {
				return err
			}
			if err := tx.Model(&order).Updates(map[string]any{"status": "canceled", "canceled_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			changed = true
			return tx.Create(&model.AuditLog{Actor: "system", Action: "commerce.funding.expire", Target: fmt.Sprintf("order:%d", id), Detail: "reservation expired and released"}).Error
		})
		if err != nil {
			return processed, err
		}
		if changed {
			processed++
		}
	}
	return
}
