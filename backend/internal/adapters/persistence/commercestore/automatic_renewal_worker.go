package commercestore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errAutomaticRenewalHeadChanged = errors.New("automatic renewal queue changed before owner lock")

func automaticRenewalHead(db *gorm.DB, now time.Time) (model.SubscriptionAutomaticRenewal, error) {
	var head model.SubscriptionAutomaticRenewal
	err := db.Table("subscription_automatic_renewals AS a").Select("a.*").
		Joins("JOIN subscriptions AS s ON s.id = a.subscription_id AND s.user_id = a.user_id").
		Where("a.enabled = ? AND a.next_attempt_at <= ? AND s.end_at <= ?", true, now, now).
		Order("a.user_id ASC, s.end_at ASC, a.subscription_id ASC").Take(&head).Error
	return head, err
}

// Every executor chooses the current first due item under the same database
// guard. The guard is held through one subscription's financial commit; the
// next transaction can then pick the next item. Insufficient funds use a bounded
// retry date, so one account cannot starve the remainder of the queue.
func (s AutomaticRenewals) Process(ctx context.Context, limit int) (processed int, err error) {
	if limit < 1 || limit > 200 {
		return 0, &commerce.ValidationError{Message: "自动续费批次必须在1至200之间。"}
	}
	for attempts := 0; attempts < limit*4 && processed < limit; attempts++ {
		prepared, err := automaticRenewalHead(s.DB.WithContext(ctx), commerceNow(s.Now))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return processed, nil
		}
		if err != nil {
			return processed, err
		}
		err = s.processAutomaticRenewal(ctx, prepared)
		if errors.Is(err, errAutomaticRenewalHeadChanged) {
			continue
		}
		if err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (s AutomaticRenewals) processAutomaticRenewal(ctx context.Context, prepared model.SubscriptionAutomaticRenewal) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// SQLite has no SELECT FOR UPDATE; acquire its writer lock before any
		// read. MySQL acquires the singleton row's current exclusive read.
		if tx.Dialector.Name() == "sqlite" {
			if err := tx.Exec("UPDATE automatic_renewal_scans SET sequence = sequence WHERE id = 1").Error; err != nil {
				return err
			}
		}
		var guard model.AutomaticRenewalScan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&guard, 1).Error; err != nil {
			return fmt.Errorf("automatic renewal ordering source missing; restore before charging: %w", err)
		}
		user, err := automaticRenewalScope(tx, prepared.UserID, prepared.SubscriptionID)
		if err != nil {
			return err
		}
		// This is the first consistent read, after current order/owner locks.
		// A different process or a consent change may have advanced the head.
		now := commerceNow(s.Now)
		head, err := automaticRenewalHead(tx, now)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (head.SubscriptionID != prepared.SubscriptionID || head.UserID != prepared.UserID)) {
			return errAutomaticRenewalHeadChanged
		}
		if err != nil {
			return err
		}
		agreement := head
		// Match ordinary settlement's plan -> SKU -> policy -> subscription
		// order. A subscription-first quote could deadlock an administrator's
		// catalog update or a concurrent manual payment waiting for that plan.
		var identity model.PlanSKU
		if err := tx.Select("id", "plan_id").First(&identity, agreement.PlanSKUID).Error; err != nil {
			return err
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&plan, identity.PlanID).Error; err != nil {
			return err
		}
		var sku model.PlanSKU
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&sku, identity.ID).Error; err != nil {
			return err
		}
		grace, err := entitlementstore.RecoveryGracePeriod(tx)
		if err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, "id = ? AND user_id = ?", head.SubscriptionID, head.UserID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&agreement, "subscription_id = ? AND user_id = ?", head.SubscriptionID, head.UserID).Error; err != nil {
			return err
		}
		now = commerceNow(s.Now)
		if !agreement.Enabled || agreement.NextAttemptAt.After(now) || sub.EndAt.After(now) {
			return errAutomaticRenewalHeadChanged
		}
		if guard.Sequence == math.MaxUint64 {
			return fmt.Errorf("automatic renewal ordering sequence exhausted")
		}
		guard.Sequence++
		if err := tx.Model(&guard).Update("sequence", guard.Sequence).Error; err != nil {
			return err
		}
		previousState, previousEnd := agreement.State, agreement.AttemptedEndAt
		agreement.LastAttemptAt, agreement.AttemptedEndAt = &now, &sub.EndAt
		agreement.AttemptSequence = guard.Sequence
		agreement.NextAttemptAt = now.Add(5 * time.Minute)
		finish := func(state, message string, disable bool) error {
			agreement.State, agreement.Message, agreement.UpdatedAt = state, message, now
			if disable {
				agreement.Enabled = false
				agreement.Revision++
			}
			if err := tx.Save(&agreement).Error; err != nil {
				return err
			}
			// The persisted status is the account's visible insufficient-funds
			// notice. Repeated scans keep it without multiplying audit notices.
			if state == previousState && previousEnd != nil && previousEnd.Equal(sub.EndAt) && state != "renewed" {
				return nil
			}
			return tx.Create(&model.AuditLog{UserID: &agreement.UserID, Actor: "system", Action: "commerce.auto_renew." + state, Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: fmt.Sprintf("sequence=%d due=%s sku=%d consent_revision=%d %s", guard.Sequence, sub.EndAt.UTC().Format(time.RFC3339Nano), agreement.PlanSKUID, agreement.Revision, message)}).Error
		}
		if user.Status != "active" {
			return finish("account_unavailable", "账户当前不可用，自动续费已停止。", true)
		}
		if !entitlements.CanRenewAt(entitlements.Subscription(sub), now, grace) {
			return finish("window_closed", "订阅已结束、主动退订或超过续费恢复窗口，自动扣款已停止。", true)
		}
		order, offer, root, digest, err := automaticRenewalQuote(tx, agreement.UserID, sub, agreement.PlanSKUID, now)
		if err != nil {
			var invalid *commerce.ValidationError
			if errors.As(err, &invalid) {
				return finish("agreement_unavailable", invalid.Message, false)
			}
			return err
		}
		if root != agreement.RootOrderID || digest != agreement.AgreementDigest {
			return finish("conditions_changed", "购买系列、续费规格或权益条件已变化，请重新核对并开启自动续费。", true)
		}
		available, err := walletAvailable(tx, agreement.UserID, order.Currency, true)
		if err != nil {
			return err
		}
		if available < order.PayableAmount {
			return finish("insufficient_balance", "可用站内余额不足，尚未扣款；请充值或手动续费，恢复窗口内每5分钟继续检查。", false)
		}
		// A known business failure must roll back order creation, reservation,
		// value capture, credentials and outbox together before saving a notice.
		if err := tx.SavePoint("automatic_renewal_payment").Error; err != nil {
			return err
		}
		key := "auto_" + walletKey(fmt.Sprintf("%d/%s/%d", sub.ID, sub.EndAt.UTC().Format(time.RFC3339Nano), sub.EntitlementEpoch))
		created, err := createOwnedOrder(tx, agreement.UserID, commerce.OrderCreateRequest{IdempotencyKey: key, PlanSKUID: agreement.PlanSKUID, TargetSubscriptionID: sub.ID, QuoteFingerprint: offer.QuoteFingerprint}, s.Now)
		if err == nil {
			err = configureOrderFunding(tx, created, commerce.FundingRequest{UseBalance: true}, commerceNow(s.Now))
		}
		if err == nil {
			payments := WalletPayments{DB: s.DB, Issuer: s.Issuer, Now: s.Now}
			_, err = payments.payReservedWalletOrder(tx, &created, agreement.UserID, true)
		}
		if err != nil {
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) && !errors.Is(err, entitlements.ErrCapacity) {
				return err
			}
			if rollback := tx.RollbackTo("automatic_renewal_payment").Error; rollback != nil {
				return rollback
			}
			return finish("retry_pending", "续费条件在付款前变化，未扣款；将在恢复窗口内重新核对。", false)
		}
		var renewed model.Subscription
		if err := tx.First(&renewed, sub.ID).Error; err != nil {
			return err
		}
		agreement.LastOrderID = &created.ID
		agreement.NextAttemptAt = renewed.EndAt
		return finish("renewed", fmt.Sprintf("已通过站内余额完成续费，订单 #%d；节点配置交付在后台核验。", created.ID), false)
	})
}
