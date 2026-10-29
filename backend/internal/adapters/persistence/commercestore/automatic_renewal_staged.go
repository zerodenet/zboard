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

func sameAutomaticPendingLink(a, b model.SubscriptionAutomaticRenewal) bool {
	if (a.PendingOrderID == nil) != (b.PendingOrderID == nil) {
		return false
	}
	if a.PendingOrderID != nil && *a.PendingOrderID != *b.PendingOrderID {
		return false
	}
	if (a.PendingEndAt == nil) != (b.PendingEndAt == nil) {
		return false
	}
	if a.PendingEndAt != nil && !a.PendingEndAt.Equal(*b.PendingEndAt) {
		return false
	}
	return a.PendingConsentRevision == b.PendingConsentRevision && a.PendingEpoch == b.PendingEpoch && a.PendingAttempt == b.PendingAttempt
}

// Called inside the existing guarded financial preparation TX. It creates one
// order+redemption intent and links the exact consent/term/epoch, but never
// configures funding, reserves wallet money, fulfills, or calls the provider.
func (s AutomaticRenewals) stageAutomaticDiscountOrder(tx *gorm.DB, row *model.SubscriptionAutomaticRenewal, sub model.Subscription, offer commerce.AutomaticRenewalOffer, accepted checkoutDiscount, now time.Time) error {
	if row.PendingOrderID != nil || row.PendingAttempt == math.MaxUint64 || row.Revision == 0 || !row.Enabled || accepted.Quote.Offer.Redemption != commerce.DiscountRedemptionGuarantee {
		return commerce.ErrDiscountRedemptionConflict
	}
	attempt := row.PendingAttempt + 1
	key := "autoq_" + walletKey(fmt.Sprintf("%d/%s/%d/%d/%d", sub.ID, sub.EndAt.UTC().Format(time.RFC3339Nano), sub.EntitlementEpoch, row.Revision, attempt))
	order, err := createOwnedOrderWithDiscount(tx, row.UserID, commerce.OrderCreateRequest{IdempotencyKey: key, PlanSKUID: row.PlanSKUID, TargetSubscriptionID: sub.ID, QuoteFingerprint: offer.QuoteFingerprint}, &accepted, s.Now)
	if err != nil {
		return err
	}
	required, _, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return err
	}
	if !required {
		return commerce.ErrDiscountRedemptionConflict
	}
	row.PendingOrderID = &order.ID
	row.PendingConsentRevision = row.Revision
	row.PendingEndAt = &sub.EndAt
	row.PendingEpoch = sub.EntitlementEpoch
	row.PendingAttempt = attempt
	row.NextAttemptAt = now.Add(15 * time.Second)
	return tx.Create(&model.AuditLog{UserID: &row.UserID, Actor: "system", Action: "commerce.auto_renew.stage_created", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: fmt.Sprintf("order=%d consent_revision=%d attempt=%d no_wallet_reservation=true no_charge=true", order.ID, row.Revision, attempt)}).Error
}

func readAutomaticStageOrder(tx *gorm.DB, id uint, lock bool) (model.Order, error) {
	if lock {
		var header model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&header, id).Error; err != nil {
			return model.Order{}, err
		}
	}
	rows, err := readOrderEvidence(tx.Model(&model.Order{}).Where("id = ?", id))
	if err != nil {
		return model.Order{}, err
	}
	if len(rows) != 1 {
		return model.Order{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

func readAutomaticStageHeader(tx *gorm.DB, id uint, lock bool) (model.Order, error) {
	var order model.Order
	q := tx.Select("id,user_id,target_subscription_id,order_type,status,amount_cents,discount_amount,payable_amount,currency,plan_id,plan_sku_id")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&order, id).Error
	return order, err
}

func clearAutomaticPending(row *model.SubscriptionAutomaticRenewal) {
	row.PendingOrderID = nil
	row.PendingConsentRevision = 0
	row.PendingEndAt = nil
	row.PendingEpoch = 0
}

// Caller holds one pending order -> owner -> consent. Cancel-only cleanup does
// not read/lock the historical source set, estimate residuals or mutate rights.
func cancelAutomaticStageLocked(tx *gorm.DB, row *model.SubscriptionAutomaticRenewal, order model.Order, sub model.Subscription, now time.Time, state, message string, disable bool) error {
	if row.PendingOrderID == nil || *row.PendingOrderID != order.ID || order.UserID != row.UserID || order.OrderType != "renewal" || order.TargetSubscriptionID == nil || *order.TargetSubscriptionID != row.SubscriptionID {
		return commerce.ErrDiscountRedemptionConflict
	}
	if order.Status == "pending" || order.Status == "failed" || order.Status == "canceled" {
		funding, configured, err := findOrderFunding(tx, order)
		if err != nil {
			return err
		}
		if configured && funding.State == "captured" {
			return commerce.ErrDiscountRedemptionConflict
		}
		// The durable65 intent is sufficient to close this one owned unpaid
		// order. Do not load its possibly oversized optional/source material.
		var intent model.OrderDiscountRedemption
		if err := loadDiscountRedemption(tx, order.ID, true, &intent); err != nil {
			return err
		}
		if err := validateDiscountReservationOrder(intent, order); err != nil {
			return err
		}
		closing := order
		closing.Status = "canceled"
		if err := ReleaseDiscountRedemptionIntent(tx, closing, now); err != nil {
			return err
		}
		if err := releaseOrderFunding(tx, order, "automatic_stage_closed", now); err != nil {
			return err
		}
		if order.Status != "canceled" {
			if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(map[string]any{"status": "canceled", "canceled_at": now, "failure_reason": message, "updated_at": now}).Error; err != nil {
				return err
			}
		}
	} else if order.Status != "paid" && order.Status != "refunded" {
		return commerce.ErrDiscountRedemptionConflict
	}
	// A paid stage may have been explicitly paid manually. Never release a used
	// coupon or label that payment as a second successful automatic renewal.
	clearAutomaticPending(row)
	if disable && row.Enabled {
		row.Enabled = false
		row.Revision++
	}
	if !row.Enabled {
		if row.State == "window_closed" || row.State == "conditions_changed" {
			state = row.State
			message = row.Message + " 旧待处理订单已关闭，未再次扣款。"
		} else if state != "window_closed" && state != "conditions_changed" {
			state = "disabled"
			message = "已关闭余额自动续费；旧待处理订单已关闭，未再次扣款。"
		}
	}
	row.State = state
	row.Message = message
	row.UpdatedAt = now
	row.NextAttemptAt = maxTime(sub.EndAt, now.Add(time.Minute))
	if err := tx.Save(row).Error; err != nil {
		return err
	}
	return tx.Create(&model.AuditLog{UserID: &row.UserID, Actor: "system", Action: "commerce.auto_renew.stage_closed", Target: fmt.Sprintf("subscription:%d", row.SubscriptionID), Detail: fmt.Sprintf("order=%d consent_revision=%d attempt=%d no_new_charge=true %s", order.ID, row.Revision, row.PendingAttempt, message)}).Error
}

func (s AutomaticRenewals) cancelAutomaticStage(ctx context.Context, prepared model.SubscriptionAutomaticRenewal, state, message string, disable bool) error {
	if prepared.PendingOrderID == nil {
		return errAutomaticRenewalHeadChanged
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		order, err := readAutomaticStageHeader(tx, *prepared.PendingOrderID, true)
		if err != nil {
			return err
		}
		var owner model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,status").First(&owner, prepared.UserID).Error; err != nil {
			return err
		}
		var row model.SubscriptionAutomaticRenewal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&row).Error; err != nil {
			return err
		}
		if row.PendingOrderID == nil || *row.PendingOrderID != *prepared.PendingOrderID || row.PendingConsentRevision != prepared.PendingConsentRevision {
			return errAutomaticRenewalHeadChanged
		}
		var sub model.Subscription
		if err := tx.Select("id,end_at").Where("id = ? AND user_id = ?", row.SubscriptionID, row.UserID).First(&sub).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return cancelAutomaticStageLocked(tx, &row, order, sub, commerceNow(s.Now), state, message, disable)
	})
}

func (s AutomaticRenewals) automaticStageNotice(ctx context.Context, prepared model.SubscriptionAutomaticRenewal, state, message string, delay time.Duration) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := orderReader(tx, prepared.UserID, false); err != nil {
			return err
		}
		var row model.SubscriptionAutomaticRenewal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&row).Error; err != nil {
			return err
		}
		if row.PendingOrderID == nil || prepared.PendingOrderID == nil || *row.PendingOrderID != *prepared.PendingOrderID || row.PendingConsentRevision != prepared.PendingConsentRevision {
			return errAutomaticRenewalHeadChanged
		}
		if (!row.Enabled || row.Revision != row.PendingConsentRevision) && state != "stage_review_required" {
			return errAutomaticRenewalHeadChanged
		}
		now := commerceNow(s.Now)
		return tx.Model(&row).Updates(map[string]any{"state": state, "message": message, "next_attempt_at": now.Add(delay), "updated_at": now}).Error
	})
}

// This checks cheap authoritative state first so disabled/stale stages always
// reach cancel-only cleanup even when unrelated history is huge or damaged.
func (s AutomaticRenewals) processAutomaticStaged(ctx context.Context, prepared model.SubscriptionAutomaticRenewal) (result error) {
	defer func() {
		if errors.Is(result, gorm.ErrRecordNotFound) || errors.Is(result, commerce.ErrDiscountRedemptionConflict) || errors.Is(result, commerce.ErrDiscountQuoteInvalid) || errors.Is(result, commerce.ErrSubscriptionEvidenceBudget) {
			result = s.automaticStageNotice(ctx, prepared, "stage_review_required", "待处理自动续费的恢复依据需要核对，未扣款；仍可关闭授权，其他订阅继续处理。", 30*time.Minute)
		}
	}()
	if prepared.PendingOrderID == nil {
		return errAutomaticRenewalHeadChanged
	}
	var current model.SubscriptionAutomaticRenewal
	if err := s.DB.WithContext(ctx).Select("subscription_id,user_id,enabled,revision,pending_order_id,pending_consent_revision,pending_end_at,pending_epoch,pending_attempt,next_attempt_at").Where("subscription_id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&current).Error; err != nil {
		return err
	}
	if current.PendingOrderID == nil || *current.PendingOrderID != *prepared.PendingOrderID || current.PendingConsentRevision != prepared.PendingConsentRevision {
		return errAutomaticRenewalHeadChanged
	}
	var sub model.Subscription
	err := s.DB.WithContext(ctx).Select("id,user_id,start_at,end_at,entitlement_epoch,status").Where("id = ? AND user_id = ?", current.SubscriptionID, current.UserID).First(&sub).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !current.Enabled || current.Revision != current.PendingConsentRevision || current.PendingConsentRevision == 0 || current.PendingAttempt == 0 {
		return s.cancelAutomaticStage(ctx, current, "waiting", "原自动扣款许可已撤销或更新，旧待处理订单已关闭；未扣款。", false)
	}
	grace, err := entitlementstore.RecoveryGracePeriod(s.DB.WithContext(ctx))
	if err != nil {
		return err
	}
	if sub.ID == 0 || !entitlements.CanRenewAt(entitlements.Subscription(sub), commerceNow(s.Now), grace) {
		return s.cancelAutomaticStage(ctx, current, "window_closed", "订阅已结束或超过恢复窗口，待处理自动续费已关闭；未扣款。", true)
	}
	if current.PendingEndAt == nil || !sub.EndAt.Equal(*current.PendingEndAt) || current.PendingEpoch != sub.EntitlementEpoch {
		return s.cancelAutomaticStage(ctx, current, "waiting", "订阅期限或权益已由其他操作变更，旧待处理自动续费已关闭；未重复扣款。", false)
	}
	var orderHead model.Order
	if err := s.DB.WithContext(ctx).Select("id,status").First(&orderHead, *current.PendingOrderID).Error; err != nil {
		return err
	}
	if orderHead.Status != "pending" {
		return s.cancelAutomaticStage(ctx, current, "waiting", "待处理订单已由其他操作关闭或结清，未重复扣款。", false)
	}
	scope, err := prepareAutomaticRenewalScope(s.DB.WithContext(ctx), current.UserID, current.SubscriptionID)
	if errors.Is(err, errAutomaticHistoryBound) || errors.Is(err, commerce.ErrSubscriptionEvidenceBudget) {
		return s.automaticStageNotice(ctx, current, "history_review_required", automaticHistoryReviewMessage, 30*time.Minute)
	}
	if err != nil {
		return err
	}
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
		user, err := automaticRenewalScope(tx, scope)
		if errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
			return errAutomaticRenewalHeadChanged
		}
		if err != nil {
			return err
		}
		now := commerceNow(s.Now)
		head, err := automaticRenewalHead(tx, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errAutomaticRenewalHeadChanged
		}
		if err != nil {
			return err
		}
		if head.SubscriptionID != prepared.SubscriptionID || head.UserID != prepared.UserID {
			return errAutomaticRenewalHeadChanged
		}
		order, err := readAutomaticStageOrder(tx, *prepared.PendingOrderID, false)
		if err != nil {
			return err
		}
		var plan model.Plan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&plan, order.PlanID).Error; err != nil {
			return err
		}
		var sku model.PlanSKU
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sku, order.PlanSKUID).Error; err != nil {
			return err
		}
		grace, err := entitlementstore.RecoveryGracePeriod(tx)
		if err != nil {
			return err
		}
		var lockedSub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&lockedSub).Error; err != nil {
			return err
		}
		var row model.SubscriptionAutomaticRenewal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&row).Error; err != nil {
			return err
		}
		if !sameAutomaticPreparation(row, prepared) {
			return errAutomaticRenewalHeadChanged
		}
		now = commerceNow(s.Now)
		if !row.Enabled || row.Revision != row.PendingConsentRevision || row.PendingEndAt == nil || !lockedSub.EndAt.Equal(*row.PendingEndAt) || row.PendingEpoch != lockedSub.EntitlementEpoch {
			return cancelAutomaticStageLocked(tx, &row, order, lockedSub, now, "waiting", "许可、期限或权益已变化，未扣款。", false)
		}
		if user.Status != "active" || !entitlements.CanRenewAt(entitlements.Subscription(lockedSub), now, grace) {
			return cancelAutomaticStageLocked(tx, &row, order, lockedSub, now, "window_closed", "账户或恢复窗口不可用，未扣款。", true)
		}
		if guard.Sequence == math.MaxUint64 {
			return fmt.Errorf("automatic renewal ordering sequence exhausted")
		}
		guard.Sequence++
		if err := tx.Model(&guard).Update("sequence", guard.Sequence).Error; err != nil {
			return err
		}
		row.AttemptSequence = guard.Sequence
		row.LastAttemptAt = &now
		row.AttemptedEndAt = &lockedSub.EndAt
		if err := validateAutomaticStagedQuote(tx, row, order, lockedSub, plan, sku, now); err != nil {
			var invalid *commerce.ValidationError
			if errors.As(err, &invalid) || errors.Is(err, commerce.ErrDiscountQuoteInvalid) || errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
				return cancelAutomaticStageLocked(tx, &row, order, lockedSub, now, "conditions_changed", "原报价、来源或授权条件已变化，旧待处理订单已关闭；请重新确认自动续费。", true)
			}
			return err
		}
		if err := RequireOrderDiscountReservation(tx, order, now); err != nil {
			open, windowErr := automaticStagePaymentWindow(tx, order, now)
			if windowErr != nil {
				return windowErr
			}
			if !open || errors.Is(err, commerce.ErrDiscountRedemptionUnsupported) {
				return cancelAutomaticStageLocked(tx, &row, order, lockedSub, now, "waiting", "本次优惠预留已失效或不受支持，未扣款；恢复窗口内将重新获取独立报价。", false)
			}
			if !errors.Is(err, commerce.ErrDiscountRedemptionPending) {
				return err
			}
			row.State = "discount_reserving"
			row.Message = "优惠预留结果尚未确认，未预留或扣除余额；正在核对原预约。"
			row.NextAttemptAt = now.Add(time.Minute)
			row.UpdatedAt = now
			return tx.Save(&row).Error
		}
		funding, configured, err := findOrderFunding(tx, order)
		if err != nil {
			return err
		}
		if configured {
			if funding.State == "captured" {
				return commerce.ErrDiscountRedemptionConflict
			}
			if funding.State != "reserved" || !funding.ExpiresAt.After(now) {
				return cancelAutomaticStageLocked(tx, &row, order, lockedSub, now, "waiting", "旧资金预留已失效，未再次扣款；恢复窗口内重新确认独立订单。", false)
			}
			if !funding.UseBalance || funding.CashMinor != 0 || funding.EntitlementMinor != 0 {
				return cancelAutomaticStageLocked(tx, &row, order, lockedSub, now, "conditions_changed", "待处理订单的资金分配已被其他操作更改，自动扣款已停止；渠道款按原订单可靠核对。", true)
			}
		}
		available, err := walletAvailable(tx, row.UserID, order.Currency, true)
		if err != nil {
			return err
		}
		if configured {
			if funding.WalletMinor > math.MaxInt64-available {
				return commerce.ErrWalletUnavailable
			}
			available += funding.WalletMinor
		}
		if available < order.PayableAmount {
			row.State = "insufficient_balance"
			row.Message = "优惠已确认，但可用余额不足，尚未扣款；恢复窗口内继续尝试。"
			row.NextAttemptAt = now.Add(5 * time.Minute)
			row.UpdatedAt = now
			return tx.Save(&row).Error
		}
		if err := tx.SavePoint("automatic_staged_payment").Error; err != nil {
			return err
		}
		err = configureOrderFunding(tx, order, commerce.FundingRequest{UseBalance: true}, commerceNow(s.Now))
		if err == nil {
			payments := WalletPayments{DB: s.DB, Issuer: s.Issuer, Now: s.Now}
			_, err = payments.payReservedWalletOrder(tx, &order, row.UserID, true)
		}
		if err != nil {
			var invalid *commerce.ValidationError
			if !errors.As(err, &invalid) && !errors.Is(err, entitlements.ErrCapacity) && !errors.Is(err, commerce.ErrSubscriptionQuoteChanged) {
				return err
			}
			if rollback := tx.RollbackTo("automatic_staged_payment").Error; rollback != nil {
				return rollback
			}
			// Rollback includes wallet/entitlement/commit/publication intent. Keep the
			// same reserved order for recoverable financial failures, never double debit.
			row.State = "retry_pending"
			row.Message = "付款前条件变化，尚未扣款；将重新核对原待处理订单。"
			row.NextAttemptAt = commerceNow(s.Now).Add(time.Minute)
			row.UpdatedAt = commerceNow(s.Now)
			return tx.Save(&row).Error
		}
		var renewed model.Subscription
		if err := tx.First(&renewed, lockedSub.ID).Error; err != nil {
			return err
		}
		row.LastOrderID = &order.ID
		clearAutomaticPending(&row)
		row.State = "renewed"
		row.Message = fmt.Sprintf("已使用余额完成续费，订单 #%d；配置交付在后台核验。", order.ID)
		row.NextAttemptAt = renewed.EndAt
		row.UpdatedAt = commerceNow(s.Now)
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &row.UserID, Actor: "system", Action: "commerce.auto_renew.renewed", Target: fmt.Sprintf("subscription:%d", row.SubscriptionID), Detail: fmt.Sprintf("order=%d sequence=%d consent_revision=%d staged_attempt=%d", order.ID, guard.Sequence, row.Revision, row.PendingAttempt)}).Error
	})
}

func automaticStagePaymentWindow(tx *gorm.DB, order model.Order, now time.Time) (bool, error) {
	var intent model.OrderDiscountRedemption
	if err := loadDiscountRedemption(tx, order.ID, true, &intent); err != nil {
		return false, err
	}
	return discountReservationWindowOpen(tx, intent, order, now)
}

// Rebuild only host facts using the baseline preparation path: it has no RPC
// and accepts no new offer. A previously accepted quote is validated at its
// original AcceptedAt, without extending its expiry or replacing its amount.
func validateAutomaticStagedQuote(tx *gorm.DB, row model.SubscriptionAutomaticRenewal, order model.Order, sub model.Subscription, plan model.Plan, sku model.PlanSKU, now time.Time) error {
	required, saved, err := OrderDiscountRedemptionRequirement(order)
	if err != nil {
		return err
	}
	if !required || saved == nil || saved.Version != 2 || saved.Source == nil || row.AgreementVersion != 2 || order.UserID != row.UserID || order.PlanSKUID != row.PlanSKUID || order.TargetSubscriptionID == nil || *order.TargetSubscriptionID != sub.ID {
		return commerce.ErrDiscountQuoteInvalid
	}
	if !plan.IsRenewable || sku.PlanID != plan.ID {
		return commerce.ErrSubscriptionQuoteChanged
	}
	if err := validateRenewalTarget(tx, order); err != nil {
		return err
	}
	approvedDigest, err := validateAutomaticRecurringApproval(row.ApprovalSnapshot, row.UserID, sub.ID, row.PlanSKUID, row.RootOrderID)
	if err != nil || approvedDigest != row.AgreementDigest || row.RootOrderID != saved.Source.RootOrderID || row.PendingConsentRevision == 0 {
		return commerce.ErrDiscountQuoteInvalid
	}
	accepted := checkoutDiscount{Version: 2, Source: saved.Source, Context: saved.Context, Quote: saved.Quote, AcceptedAt: saved.AcceptedAt}
	approval, digest, err := automaticRecurringApproval(order, accepted)
	if err != nil || approval != row.ApprovalSnapshot || digest != row.AgreementDigest {
		return commerce.ErrSubscriptionQuoteChanged
	}
	baseline := &checkoutDiscount{Baseline: true}
	fresh, _, err := prepareOrderWithDiscount(tx, row.UserID, commerce.OrderCreateRequest{PlanSKUID: row.PlanSKUID, TargetSubscriptionID: sub.ID}, now, baseline)
	if err != nil {
		return err
	}
	fresh.PlanName = order.PlanName
	fresh.SKUName = order.SKUName // accepted display labels are not new debit terms
	policy, err := commerce.DecodePurchaseSnapshot(fresh.PurchaseSnapshot)
	if err != nil {
		return err
	}
	context, err := commerce.NewRecurringDiscountContext(commerce.Order(fresh), policy, saved.Context.Selection, baseline.Source)
	if err != nil {
		return err
	}
	original, _ := json.Marshal(saved.Context)
	current, _ := json.Marshal(context)
	if string(original) != string(current) {
		return commerce.ErrSubscriptionQuoteChanged
	}
	return nil
}

// This runs only AFTER the financial/preparation TX has returned. Backoff is
// based on the clock after RPC, conditionally bound to the same pending link and
// consent revision, so slow RPC cannot repeatedly reclaim the queue's first item.
func (s AutomaticRenewals) reconcileAutomaticStageOutsideTransaction(ctx context.Context, prepared model.SubscriptionAutomaticRenewal) error {
	var row model.SubscriptionAutomaticRenewal
	if err := s.DB.WithContext(ctx).Select("subscription_id,user_id,enabled,revision,pending_order_id,pending_consent_revision,state").Where("subscription_id = ? AND user_id = ?", prepared.SubscriptionID, prepared.UserID).First(&row).Error; err != nil {
		return err
	}
	if row.PendingOrderID == nil || !row.Enabled || row.Revision != row.PendingConsentRevision {
		return nil
	}
	if row.State != "discount_reserving" && row.State != "discount_reserved" {
		return nil
	}
	provider, ok := s.Discounts.(commerce.DiscountRedemptionProvider)
	var rpcErr error
	if !ok {
		rpcErr = commerce.ErrDiscountRedemptionUnsupported
	} else {
		rpcErr = (DiscountRedemptionJobs{DB: s.DB, Provider: provider, Now: s.Now}).Step(ctx, *row.PendingOrderID)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	now := commerceNow(s.Now)
	state, message := "discount_reserving", "优惠预留仍在核对，尚未预留或扣除余额；不会恢复原价自动扣款。"
	order, readErr := readAutomaticStageOrder(s.DB.WithContext(ctx), *row.PendingOrderID, false)
	if readErr == nil {
		view, e := automaticStageReservationView(s.DB.WithContext(ctx), order, now)
		if e != nil {
			readErr = e
		} else if view != nil && view.CanPay {
			state = "discount_reserved"
			message = "优惠已确认，尚未扣除余额；下一次核对许可、权益及余额后续费。"
		}
	}
	update := s.DB.WithContext(ctx).Model(&model.SubscriptionAutomaticRenewal{}).Where("subscription_id = ? AND user_id = ? AND pending_order_id = ? AND pending_consent_revision = ? AND revision = ? AND enabled = ?", row.SubscriptionID, row.UserID, *row.PendingOrderID, row.PendingConsentRevision, row.PendingConsentRevision, true).Updates(map[string]any{"state": state, "message": message, "next_attempt_at": now.Add(time.Minute), "updated_at": now})
	return errors.Join(rpcErr, readErr, update.Error)
}

// A status read for a persisted stage does not need another provider quote or a
// whole-history scan. It cannot replay payment, grant fresh consent or replace
// the accepted amount. Changing to another SKU still uses the normal fresh offer.
func (s AutomaticRenewals) readAutomaticPendingView(ctx context.Context, owner, subscription, sku uint) (out commerce.AutomaticRenewalView, present bool, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, e := ownedAutomaticConsent(tx, owner, subscription, false)
		if e != nil {
			return e
		}
		if row.PendingOrderID == nil || (sku != 0 && sku != row.PlanSKUID) {
			return nil
		}
		present = true
		out = automaticRenewalView(row, subscription)
		if !row.Enabled || row.Revision != row.PendingConsentRevision {
			out.Message = "当前自动扣款许可已关闭或更新，旧待处理订单不会自动扣款；清理和优惠释放在后台推进。"
			return nil
		}
		header, e := readAutomaticStageHeader(tx, *row.PendingOrderID, false)
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e != nil || header.UserID != owner || header.TargetSubscriptionID == nil || *header.TargetSubscriptionID != subscription {
			out.PendingOrderID = nil
			out.State = "stage_review_required"
			out.Message = "待处理订单关联需要核对，未扣款；仍可关闭自动续费。"
			return nil
		}
		if header.Status == "paid" || header.Status == "refunded" {
			out.State = "stage_settled"
			out.Message = "关联订单已由其他操作结清，后台会核对并清理关联，不会重复自动扣款。"
			return nil
		}
		if header.Status == "canceled" || header.Status == "failed" {
			out.State = "stage_closed"
			out.Message = "关联订单已关闭或失败，后台将清理旧预留；不会用此订单自动扣款。"
			return nil
		}
		order, e := readAutomaticStageOrder(tx, *row.PendingOrderID, false)
		if e == nil {
			view, viewErr := automaticStageReservationView(tx, order, commerceNow(s.Now))
			e = viewErr
			if e == nil && view != nil {
				if view.CanPay {
					if row.State == "insufficient_balance" || row.State == "history_review_required" || row.State == "stage_review_required" || row.State == "retry_pending" {
						return nil
					}
					out.State = "discount_reserved"
					out.Message = "已确认原订单优惠，尚未自动扣款；后台将重新核对许可、权益和余额，刷新不会发起付款。"
				} else if view.State == "closed" || view.State == "unavailable" {
					out.State = "discount_unavailable"
					out.Message = view.Message
				} else {
					out.State = "discount_reserving"
					out.Message = "原订单优惠预留正在确认，尚未扣款；不会恢复原价自动扣款。"
				}
			}
		}
		if errors.Is(e, commerce.ErrSubscriptionEvidenceBudget) || errors.Is(e, commerce.ErrDiscountRedemptionConflict) || errors.Is(e, commerce.ErrDiscountQuoteInvalid) {
			out.State = "stage_review_required"
			out.Message = "待处理自动续费依据需要核对，未扣款；仍可关闭授权。"
			return nil
		}
		return e
	})
	return
}

func automaticStageReservationView(tx *gorm.DB, order model.Order, now time.Time) (*commerce.DiscountReservationView, error) {
	funding, configured, err := findOrderFunding(tx, order)
	if err != nil {
		return nil, err
	}
	cash := order.PayableAmount
	if configured {
		cash = funding.CashMinor
	}
	return orderDiscountReservationView(tx, order, now, cash)
}
