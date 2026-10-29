package commercestore

import (
	"context"
	"encoding/json"
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

const automaticHistoryReviewMessage = "待处理订单或成交证据超过单次处理预算，本项自动扣款已暂停且未扣款；仍可关闭授权。请通过工单核对历史，系统仅在恢复窗口内每30分钟重新检查。"

func automaticRenewalHead(db *gorm.DB, now time.Time) (model.SubscriptionAutomaticRenewal, error) {
	var head model.SubscriptionAutomaticRenewal
	err := db.Table("subscription_automatic_renewals AS a").Select("a.*").
		Joins("JOIN subscriptions AS s ON s.id = a.subscription_id AND s.user_id = a.user_id").
		Where("a.next_attempt_at <= ? AND ((a.enabled = ? AND s.end_at <= ?) OR a.pending_order_id IS NOT NULL)", now, true, now).
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
	var stageFailures error
	for attempts := 0; attempts < limit*4 && processed < limit; attempts++ {
		prepared, err := automaticRenewalHead(s.DB.WithContext(ctx), commerceNow(s.Now))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return processed, stageFailures
		}
		if err != nil {
			return processed, err
		}
		if prepared.PendingOrderID != nil {
			err = s.processAutomaticStaged(ctx, prepared)
			if errors.Is(err, errAutomaticRenewalHeadChanged) {
				continue
			}
			if err != nil {
				return processed, err
			}
			if err := s.reconcileAutomaticStageOutsideTransaction(ctx, prepared); err != nil {
				if ctx.Err() != nil {
					return processed, ctx.Err()
				}
				stageFailures = errors.Join(stageFailures, err)
			}
			processed++
			continue
		}
		var evaluated *automaticDiscountEvaluation
		var evaluationErr error
		if prepared.AgreementVersion == 2 {
			evaluated, evaluationErr = s.evaluateAutomaticDiscount(ctx, prepared.UserID, prepared.SubscriptionID, prepared.PlanSKUID)
			if ctx.Err() != nil {
				return processed, ctx.Err()
			}
		}
		err = s.processAutomaticRenewalPrepared(ctx, prepared, evaluated, evaluationErr)
		if errors.Is(err, errAutomaticHistoryBound) || errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
			// The financial transaction has rolled back and released its order
			// locks. Isolate a known bounded-history item without pretending its
			// money or entitlement has been reconciled.
			err = s.pauseAutomaticHistoryItem(ctx, prepared)
		}
		if errors.Is(err, errAutomaticRenewalHeadChanged) {
			continue
		}
		if err != nil {
			return processed, err
		}
		if err := s.reconcileAutomaticStageOutsideTransaction(ctx, prepared); err != nil {
			if ctx.Err() != nil {
				return processed, ctx.Err()
			}
			stageFailures = errors.Join(stageFailures, err)
		}
		processed++
	}
	return processed, stageFailures
}

func (s AutomaticRenewals) pauseAutomaticHistoryItem(ctx context.Context, prepared model.SubscriptionAutomaticRenewal) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "sqlite" {
			if err := tx.Exec("UPDATE automatic_renewal_scans SET sequence = sequence WHERE id = 1").Error; err != nil {
				return err
			}
		}
		var guard model.AutomaticRenewalScan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&guard, 1).Error; err != nil {
			return err
		}
		grace, err := entitlementstore.RecoveryGracePeriod(tx)
		if err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&sub).Error; err != nil {
			return err
		}
		now := commerceNow(s.Now)
		head, err := automaticRenewalHead(tx, now)
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (head.SubscriptionID != prepared.SubscriptionID || head.UserID != prepared.UserID)) {
			return errAutomaticRenewalHeadChanged
		}
		if err != nil {
			return err
		}
		var row model.SubscriptionAutomaticRenewal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&row).Error; err != nil {
			return err
		}
		if !row.Enabled || row.NextAttemptAt.After(now) {
			return errAutomaticRenewalHeadChanged
		}
		if guard.Sequence == math.MaxUint64 {
			return fmt.Errorf("automatic renewal ordering sequence exhausted")
		}
		guard.Sequence++
		if err := tx.Model(&guard).Update("sequence", guard.Sequence).Error; err != nil {
			return err
		}
		row.State, row.Message = "history_review_required", automaticHistoryReviewMessage
		row.LastAttemptAt, row.AttemptedEndAt = &now, &sub.EndAt
		row.AttemptSequence, row.NextAttemptAt, row.UpdatedAt = guard.Sequence, now.Add(30*time.Minute), now
		if !entitlements.CanRenewAt(entitlements.Subscription(sub), now, grace) {
			row.Enabled = false
			row.Revision++
			row.State, row.Message = "window_closed", "订阅已结束或超过恢复窗口，自动扣款已停止。"
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &row.UserID, Actor: "system", Action: "commerce.auto_renew." + row.State, Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: fmt.Sprintf("sequence=%d consent_revision=%d no_charge=true", guard.Sequence, row.Revision)}).Error
	})
}

func (s AutomaticRenewals) processAutomaticRenewalPrepared(ctx context.Context, prepared model.SubscriptionAutomaticRenewal, evaluated *automaticDiscountEvaluation, evaluationErr error) error {
	scope, scopeErr := prepareAutomaticRenewalScope(s.DB.WithContext(ctx), prepared.UserID, prepared.SubscriptionID)
	if scopeErr != nil {
		return scopeErr
	}
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
		user, err := automaticRenewalScope(tx, scope)
		if errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
			// A competing manual/automatic action may have advanced this head
			// after unlocked preparation. Roll back and reselect under the
			// existing bounded attempts loop; never reuse its old source set.
			return errAutomaticRenewalHeadChanged
		}
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, identity.PlanID).Error; err != nil {
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
		if !sameAutomaticPreparation(agreement, prepared) {
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
		var accepted *checkoutDiscount
		if agreement.AgreementVersion == 2 {
			digest, err := validateAutomaticRecurringApproval(agreement.ApprovalSnapshot, agreement.UserID, sub.ID, agreement.PlanSKUID, agreement.RootOrderID)
			if err != nil || digest != agreement.AgreementDigest {
				return finish("conditions_changed", "已批准的长期优惠扣款证据缺失或损坏，请重新核对授权。", true)
			}
			var approved automaticRecurringApprovalSnapshot
			if json.Unmarshal([]byte(agreement.ApprovalSnapshot), &approved) != nil {
				return finish("conditions_changed", "长期优惠授权证据无法核验，请重新确认。", true)
			}
			var currentSKU model.PlanSKU
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentSKU, agreement.PlanSKUID).Error; err != nil {
				return err
			}
			root, err := renewalPurchase(tx, sub.ID, agreement.UserID, sub.PlanID)
			if err != nil {
				return finish("agreement_unavailable", "已购长期优惠来源暂不可核验，未扣款。", false)
			}
			if !plan.IsRenewable || root.ID != approved.RootOrderID || currentSKU.Currency != approved.Currency || currentSKU.BillingUnit != approved.BillingUnit || currentSKU.BillingValue != approved.BillingValue || currentSKU.RenewalEffect != approved.RenewalEffect {
				return finish("conditions_changed", "购买系列或续费周期已变化，请重新核对授权。", true)
			}
			if errors.Is(evaluationErr, commerce.ErrDiscountConfirmationRequired) || errors.Is(evaluationErr, errAutomaticConditionsChanged) {
				return finish("conditions_changed", "长期优惠需要重新明确确认，自动扣款已停止。", true)
			}
			if evaluationErr != nil || evaluated == nil || !hasCheckoutOffer(evaluated.Discount) {
				return finish("agreement_unavailable", "长期优惠暂不可核验，未扣款；恢复窗口内每5分钟重新检查，仍可关闭授权。", false)
			}
			if evaluated.Digest != agreement.AgreementDigest || evaluated.Approval != agreement.ApprovalSnapshot {
				return finish("conditions_changed", "长期优惠价格、条款或提供方配置已变化，请重新核对并开启自动续费。", true)
			}
			accepted = evaluated.Discount
		} else if agreement.AgreementVersion != 1 {
			return finish("conditions_changed", "自动扣款协议版本不受支持，请重新核对授权。", true)
		} else if agreement.ApprovalSnapshot != "" {
			return finish("conditions_changed", "自动扣款协议证据与版本不一致，请重新核对授权。", true)
		}
		order, offer, root, digest, err := automaticRenewalQuote(tx, agreement.UserID, sub, agreement.PlanSKUID, now, accepted)
		if err != nil {
			if accepted != nil {
				var invalid *commerce.ValidationError
				if errors.As(err, &invalid) || errors.Is(err, commerce.ErrSubscriptionQuoteChanged) || errors.Is(err, commerce.ErrDiscountQuoteInvalid) {
					return errAutomaticRenewalHeadChanged
				}
			}
			var invalid *commerce.ValidationError
			if errors.As(err, &invalid) {
				return finish("agreement_unavailable", invalid.Message, false)
			}
			return err
		}
		if root != agreement.RootOrderID || digest != agreement.AgreementDigest {
			return finish("conditions_changed", "购买系列、续费规格或权益条件已变化，请重新核对并开启自动续费。", true)
		}
		if hasCheckoutOffer(accepted) && accepted.Quote.Offer.Redemption != "" {
			if err := tx.SavePoint("automatic_stage_prepare").Error; err != nil {
				return err
			}
			if err := s.stageAutomaticDiscountOrder(tx, &agreement, sub, offer, *accepted, now); err != nil {
				var invalid *commerce.ValidationError
				if !errors.As(err, &invalid) && !errors.Is(err, commerce.ErrDiscountQuoteInvalid) && !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
					return err
				}
				if rollback := tx.RollbackTo("automatic_stage_prepare").Error; rollback != nil {
					return rollback
				}
				return finish("retry_pending", "创建待处理续费前报价条件变化，尚未扣款；恢复窗口内重新核对。", false)
			}
			return finish("discount_reserving", "已创建独立待处理续费订单，正在确认优惠预留；尚未预留或扣除余额。", false)
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
		created, err := createOwnedOrderWithDiscount(tx, agreement.UserID, commerce.OrderCreateRequest{IdempotencyKey: key, PlanSKUID: agreement.PlanSKUID, TargetSubscriptionID: sub.ID, QuoteFingerprint: offer.QuoteFingerprint}, accepted, s.Now)
		if err == nil {
			err = configureOrderFunding(tx, created, commerce.FundingRequest{UseBalance: true}, commerceNow(s.Now))
		}
		if err == nil {
			payments := WalletPayments{DB: s.DB, Issuer: s.Issuer, Now: s.Now}
			_, err = payments.payReservedWalletOrder(tx, &created, agreement.UserID, true)
		}
		if err != nil {
			var invalid *commerce.ValidationError
			discountChanged := accepted != nil && (errors.Is(err, commerce.ErrDiscountQuoteInvalid) || errors.Is(err, commerce.ErrSubscriptionQuoteChanged))
			if !errors.As(err, &invalid) && !errors.Is(err, entitlements.ErrCapacity) && !discountChanged {
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
