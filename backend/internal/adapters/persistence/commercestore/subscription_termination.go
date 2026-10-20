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
	if !creationKeyPattern.MatchString(in.IdempotencyKey) {
		return out, &commerce.ValidationError{Message: "退订请求标识无效。"}
	}
	key := walletKey(fmt.Sprintf("unsubscribe:%d:%s", actor, in.IdempotencyKey))
	encoded, _ := json.Marshal(struct {
		Subscription uint
		Quote        string
	}{id, in.QuoteFingerprint})
	fingerprint := walletKey(string(encoded))
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Match trusted payment order-before-account locking. A new concurrent order
		// may force a retry; an aborted transaction cannot partially settle value.
		var orders []model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND (subscription_id = ? OR target_subscription_id = ?)", actor, id, id).Order("id ASC").Limit(1001).Find(&orders).Error; err != nil {
			return err
		}
		if len(orders) > 1000 {
			return commerce.ErrSubscriptionValueUnavailable
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "email", "status").First(&user, actor).Error; err != nil {
			return resourceError(err)
		}
		if user.Status != "active" {
			return commerce.ErrOrderPermission
		}
		var old model.SubscriptionValueSettlement
		lookupErr := tx.Where("request_key = ?", key).First(&old).Error
		if lookupErr == nil {
			if old.UserID != actor || old.SubscriptionID != id || old.RequestFingerprint != fingerprint {
				return commerce.ErrSubscriptionSettlementConflict
			}
			out = terminationReceipt(old)
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, actor).First(&sub).Error; err != nil {
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
		if err := tx.Model(&model.Order{}).Where("user_id = ? AND (subscription_id = ? OR target_subscription_id = ?)", actor, id, id).Order("id ASC").Limit(1001).Pluck("id", &currentIDs).Error; err != nil {
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
		if projection.Fingerprint != in.QuoteFingerprint {
			return commerce.ErrSubscriptionQuoteChanged
		}
		payload, err := json.Marshal(struct {
			Value   commerce.ValueProjection
			Sources map[uint]int64
		}{projection, projection.Sources})
		if err != nil {
			return err
		}
		row := model.SubscriptionValueSettlement{UserID: actor, SubscriptionID: id, RootOrderID: projection.RootOrderID, RequestKey: key, RequestFingerprint: fingerprint, Currency: currency, Kind: "unsubscribe", AmountMinor: projection.TotalMinor, WalletMinor: projection.TotalMinor, ActorID: actor, Reason: "用户确认自助退订，残值退入站内余额", QuoteSnapshot: string(payload), CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		total, err := refundValueSources(tx, actor, currency, row.ID, projection.Sources, orders, now)
		if err != nil {
			return err
		}
		if total != projection.TotalMinor {
			return commerce.ErrSubscriptionValueUnavailable
		}
		if total > 0 {
			entryID, err := appendWalletEntry(tx, walletEntrySource{UserID: actor, Currency: currency}, walletKey(fmt.Sprintf("unsubscribe-settlement:%d", row.ID)), "subscription_refund", "自助退订残值退款", total, 0, now)
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
		if err := tx.Create(&model.AuditLog{UserID: &actor, Actor: user.Email, Action: "subscription.unsubscribe", Target: fmt.Sprintf("subscription:%d", id), Detail: fmt.Sprintf("settlement=%d currency=%s wallet_minor=%d delivery=pending_verification", row.ID, currency, total)}).Error; err != nil {
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
