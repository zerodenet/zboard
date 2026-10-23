package commercestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/entitlementstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/commerce"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"time"
)

type SubscriptionTermination struct {
	DB  *gorm.DB
	Now func() time.Time
}

func terminationReceipt(row model.SubscriptionValueSettlement) commerce.SubscriptionTerminationReceipt {
	return commerce.SubscriptionTerminationReceipt{ID: row.ID, SubscriptionID: row.SubscriptionID, Currency: row.Currency, AmountMinor: row.WalletMinor, WalletEntryID: row.EntryID, Status: "canceled", RevocationStatus: "pending_verification", CreatedAt: row.CreatedAt}
}
func (s SubscriptionTermination) Terminate(ctx context.Context, actor, id uint, in commerce.SubscriptionTerminationRequest) (out commerce.SubscriptionTerminationReceipt, err error) {
	return s.terminate(ctx, actor, actor, id, in, nil)
}

func (s SubscriptionTermination) TerminateForAdministrator(ctx context.Context, actor, id uint, in commerce.AdministrativeSubscriptionTerminationRequest) (commerce.SubscriptionTerminationReceipt, error) {
	// Read authorization/owner outside the financial transaction; permission and
	// immutable ownership are rechecked under its existing source -> users -> sub locks.
	if err := orderReader(s.DB.WithContext(ctx), actor, true); err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	var sub model.Subscription
	if err := s.DB.WithContext(ctx).Select("id", "user_id").First(&sub, id).Error; err != nil {
		return commerce.SubscriptionTerminationReceipt{}, resourceError(err)
	}
	return s.terminate(ctx, actor, sub.UserID, id, in.SubscriptionTerminationRequest, &in)
}

func (s SubscriptionTermination) terminate(ctx context.Context, actor, owner, id uint, in commerce.SubscriptionTerminationRequest, administrative *commerce.AdministrativeSubscriptionTerminationRequest) (out commerce.SubscriptionTerminationReceipt, err error) {
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return out, &commerce.ValidationError{Message: "退订请求标识无效。"}
	}
	key := walletKey(fmt.Sprintf("unsubscribe:%d:%s", actor, in.IdempotencyKey))
	encoded, _ := json.Marshal(struct {
		Subscription uint
		Quote        string
	}{id, in.QuoteFingerprint})
	if administrative != nil {
		key = walletKey(fmt.Sprintf("admin-unsubscribe:%d:%s", actor, in.IdempotencyKey))
		encoded, _ = json.Marshal(struct {
			Subscription uint
			Input        commerce.AdministrativeSubscriptionTerminationRequest
		}{id, *administrative})
	}
	fingerprint := walletKey(string(encoded))
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Match trusted payment order-before-account locking. A new concurrent order
		// may force a retry; an aborted transaction cannot partially settle value.
		var orders []model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND (subscription_id = ? OR target_subscription_id = ?)", owner, id, id).Order("id ASC").Limit(1001).Find(&orders).Error; err != nil {
			return err
		}
		if len(orders) > 1000 {
			return commerce.ErrSubscriptionValueUnavailable
		}
		var user model.User
		if administrative != nil {
			var authErr error
			user, authErr = lockSettlementUsers(tx, owner, actor)
			if authErr != nil {
				return authErr
			}
		} else {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "email", "status").First(&user, owner).Error; err != nil {
				return resourceError(err)
			}
			if user.Status != "active" {
				return commerce.ErrOrderPermission
			}
		}
		var old model.SubscriptionValueSettlement
		lookupErr := tx.Where("request_key = ?", key).First(&old).Error
		if lookupErr == nil {
			if old.UserID != owner || old.ActorID != actor || old.SubscriptionID != id || old.RequestFingerprint != fingerprint {
				return commerce.ErrSubscriptionSettlementConflict
			}
			out = terminationReceipt(old)
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, owner).First(&sub).Error; err != nil {
			return resourceError(err)
		}
		if sub.Status == "canceled" {
			return commerce.ErrSubscriptionSettlementConflict
		}
		if sub.Status != "active" && sub.Status != "expired" {
			return commerce.ErrSubscriptionValueUnavailable
		}
		// Once the account is locked, checkout cannot add another target order.
		// Detect insertions between the initial order locks and account lock;
		// READ COMMITTED does not provide an InnoDB gap-lock guarantee.
		var currentIDs []uint
		if err := tx.Model(&model.Order{}).Where("user_id = ? AND (subscription_id = ? OR target_subscription_id = ?)", owner, id, id).Order("id ASC").Limit(1001).Pluck("id", &currentIDs).Error; err != nil {
			return err
		}
		if len(currentIDs) != len(orders) {
			return commerce.ErrSubscriptionQuoteChanged
		}
		for i, orderID := range currentIDs {
			if orders[i].ID != orderID {
				return commerce.ErrSubscriptionQuoteChanged
			}
		}
		now := commerceNow(s.Now)
		projection, currency, err := readSubscriptionValue(tx, sub, now)
		if err != nil {
			return err
		}
		defaultMinor := projection.TotalMinor
		sources := projection.Sources
		quoteFingerprint := projection.Fingerprint
		if administrative != nil {
			limits, maxRefund, err := administrativeRefundLimits(tx, sub, projection, currency)
			if err != nil {
				return err
			}
			quoteFingerprint = administrativeValueFingerprint(projection.Fingerprint, limits, maxRefund)
			amount := defaultMinor
			if administrative.AmountMinor != nil {
				amount = *administrative.AmountMinor
			}
			sources, err = commerce.AllocateAdministrativeRefund(amount, projection.Sources, limits)
			if err != nil {
				return err
			}
		}
		if quoteFingerprint != in.QuoteFingerprint {
			return commerce.ErrSubscriptionQuoteChanged
		}
		payload, err := json.Marshal(struct {
			Value   commerce.ValueProjection
			Sources map[uint]int64
		}{projection, sources})
		if err != nil {
			return err
		}
		amount := defaultMinor
		if administrative != nil && administrative.AmountMinor != nil {
			amount = *administrative.AmountMinor
		}
		reason := "用户确认自助退订，残值退入站内余额"
		if administrative != nil {
			reason = administrative.Reason
		}
		row := model.SubscriptionValueSettlement{UserID: owner, SubscriptionID: id, RootOrderID: projection.RootOrderID, RequestKey: key, RequestFingerprint: fingerprint, Currency: currency, Kind: "unsubscribe", AmountMinor: amount, WalletMinor: amount, ActorID: actor, Reason: reason, QuoteSnapshot: string(payload), CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		total, err := refundValueSources(tx, owner, currency, row.ID, sources, orders, now)
		if err != nil {
			return err
		}
		if total != amount {
			return commerce.ErrSubscriptionValueUnavailable
		}
		if total > 0 {
			entryReason := "自助退订残值退款"
			if administrative != nil {
				entryReason = "管理员整份退订退款：" + administrative.Reason
			}
			entryID, err := appendWalletEntry(tx, walletEntrySource{UserID: owner, Currency: currency}, walletKey(fmt.Sprintf("unsubscribe-settlement:%d", row.ID)), "subscription_refund", entryReason, total, 0, now)
			if err != nil {
				return err
			}
			row.EntryID = &entryID
			if err := tx.Model(&row).Update("entry_id", entryID).Error; err != nil {
				return err
			}
		}
		// Explicit unsubscribe has no recovery window. Retain cumulative identities
		// and history; verified cash arriving after cancellation cannot revive it.
		finalTotal, finalUsed := entitlements.CycleQuota(entitlements.Subscription(sub))
		for _, order := range orders {
			if order.SubscriptionEndedAt != nil && order.Status != "pending" {
				continue
			} // Preserve earlier terminated-term history.
			updates := map[string]any{"subscription_ended_at": now, "subscription_end_reason": "canceled", "subscription_final_flow_total": finalTotal, "subscription_final_flow_used": finalUsed, "updated_at": now}
			if order.Status == "pending" {
				if err := releaseOrderFunding(tx, order, "canceled", now); err != nil {
					return err
				}
				updates["status"] = "canceled"
				updates["canceled_at"] = now
			}
			if err := tx.Model(&model.Order{}).Where("id = ?", order.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := entitlementstore.TerminateInTransaction(tx, sub, strconv.FormatUint(uint64(row.ID), 10), now); err != nil {
			return err
		}
		if err := stopAutomaticRenewal(tx, sub.ID, "window_closed", "订阅已主动退订，自动扣款已停止。", now); err != nil {
			return err
		}
		detail := fmt.Sprintf("settlement=%d currency=%s wallet_minor=%d delivery=pending_verification", row.ID, currency, total)
		if administrative != nil {
			detail += fmt.Sprintf(" actor_id=%d owner_id=%d default_minor=%d actual_minor=%d reason=%q", actor, owner, defaultMinor, total, administrative.Reason)
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: user.Email, Action: "subscription.unsubscribe", Target: fmt.Sprintf("subscription:%d", id), Detail: detail}).Error; err != nil {
			return err
		}
		out = terminationReceipt(row)
		return nil
	})
	if err != nil {
		return commerce.SubscriptionTerminationReceipt{}, err
	}
	return out, nil
}
