package entitlementstore

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func subscriptionLifecycleDue(query *gorm.DB, now time.Time, configuredGrace ...time.Duration) *gorm.DB {
	grace := entitlements.RenewalGracePeriod
	if len(configuredGrace) > 0 {
		grace = configuredGrace[0]
	}
	return query.Where("(retirement_retry_at IS NULL OR retirement_retry_at <= ?)", now).Where(`(status IN ('canceled','ended') OR (status IN ('active', 'expired') AND (
  (ended_at IS NULL AND (end_at <= ? OR (ends_on_quota_exhaustion = ? AND flow_used >= flow_total)))
  OR (ended_at IS NOT NULL AND (lifecycle <> 'renewable' OR ended_at <= ?
    OR EXISTS (SELECT 1 FROM orders WHERE (orders.subscription_id = subscriptions.id OR orders.target_subscription_id = subscriptions.id) AND orders.subscription_ended_at IS NULL)
    OR EXISTS (SELECT 1 FROM protocol_credentials WHERE protocol_credentials.subscription_id = subscriptions.id AND protocol_credentials.status IN ('active', 'prepared')))))))`, now, true, now.Add(-grace))
}

func endSubscription(tx *gorm.DB, sub *model.Subscription, now time.Time) error {
	ended, reason := sub.EndAt, "expired"
	if sub.EndedAt != nil {
		ended, reason = *sub.EndedAt, sub.EndReason
	}
	if sub.EndedAt == nil && sub.EndAt.After(now) {
		ended, reason = now, "exhausted"
	}
	if sub.EndedAt == nil && (sub.Status == "canceled" || sub.Status == "ended") {
		if ended.After(now) {
			ended = now
		}
		reason = sub.EndReason
		if reason == "" {
			reason = "canceled"
		}
	}
	sub.Status, sub.EndedAt, sub.EndReason = "expired", &ended, reason
	if err := tx.Model(sub).Updates(map[string]any{"status": sub.Status, "ended_at": ended, "end_reason": reason, "updated_at": now}).Error; err != nil {
		return err
	}
	total, used := entitlements.CycleQuota(entitlements.Subscription(*sub))
	// Settlement owns the affected orders before it owns the subscription. Read
	// their identities, then update by primary key: an OR-based UPDATE can scan
	// and lock unrelated orders in MySQL, reversing that established lock order.
	var after uint
	count := 0
	for {
		var orderIDs []uint
		if err := tx.Model(&model.Order{}).Where("(subscription_id = ? OR target_subscription_id = ?) AND subscription_ended_at IS NULL AND id > ?", sub.ID, sub.ID, after).
			Order("id ASC").Limit(entitlements.LifecycleOrderPageSize).Pluck("id", &orderIDs).Error; err != nil {
			return err
		}
		if len(orderIDs) == 0 {
			return nil
		}
		for _, id := range orderIDs {
			if id <= after {
				return fmt.Errorf("expiry order identity cursor did not advance")
			}
			if count >= entitlements.LifecycleOrderIdentityByteLimit/8 {
				return entitlements.ErrCredentialExpiryScopeBudget
			}
			after = id
			count++
		}
		if err := tx.Model(&model.Order{}).Where("id IN ?", orderIDs).
			Updates(map[string]any{"subscription_ended_at": ended, "subscription_end_reason": reason, "subscription_final_flow_total": total, "subscription_final_flow_used": used}).Error; err != nil {
			return err
		}
	}
}

// Retire a business instance without deleting identities needed for late
// metering/replay or financial reconciliation. Credentials are unusable, while
// their stable attribution keys and cumulative cursors remain available.
func retireSubscription(tx *gorm.DB, sub model.Subscription, cipher entitlements.AccessCipher, now time.Time) error {
	if cipher == nil {
		return entitlements.ErrCredentialExpiryUnavailable
	}
	var credentials []model.ProtocolCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ?", sub.ID).Find(&credentials).Error; err != nil {
		return err
	}
	for _, credential := range credentials {
		hash := credential.RetiredPrincipalHash
		if credential.Secret != "" {
			plain, err := cipher.Decrypt(credential.Secret)
			if err != nil {
				return fmt.Errorf("retire credential %d: %w", credential.ID, err)
			}
			hash = entitlements.RetiredPrincipalHash(credential.NodeID, plain)
		}
		if err := tx.Model(&model.ProtocolCredential{}).Where("id = ?", credential.ID).Updates(map[string]any{"retired_principal_hash": hash}).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.ProtocolCredential{}).Where("subscription_id = ?", sub.ID).Updates(map[string]any{"status": "revoked", "revoked_at": now, "secret": "", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.SubscriptionToken{}).Where("subscription_id = ?", sub.ID).Updates(map[string]any{"revoked_at": now, "token_ciphertext": "", "updated_at": now}).Error; err != nil {
		return err
	}
	// Freeze only the existing compact accounting budget. No renewable service,
	// reset schedule, price, configuration or inventory survives this boundary.
	if sub.FlowUsed < 0 || sub.FlowUsed > sub.FlowTotal {
		return errors.New("invalid closing accounting budget")
	}
	closed := model.ClosedEntitlement{SubscriptionID: sub.ID, Epoch: sub.EntitlementEpoch, UserID: sub.UserID,
		FlowTotal: sub.FlowTotal, ClosingFlowUsed: sub.FlowUsed, FlowUsed: sub.FlowUsed, TrafficCalcMode: sub.TrafficCalcMode,
		StartAt: sub.StartAt, EndAt: sub.EndAt, ClosedAt: now, UpdatedAt: now}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&closed)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("live subscription already has a closed current accounting epoch")
	}
	// Preserve the account inactivity anchor even when its last live service or
	// shared membership is physically removed. One scalar per existing owner,
	// with no countdown rows, quota maintenance or new history table.
	ended := sub.EndAt
	if sub.EndedAt != nil {
		ended = *sub.EndedAt
	}
	if ended.After(now) {
		ended = now
	}
	ends := map[uint]time.Time{sub.UserID: ended}
	var members []model.SubscriptionMember
	if e := tx.Where("subscription_id=?", sub.ID).Find(&members).Error; e != nil {
		return e
	}
	for _, m := range members {
		at := ended
		if m.RemovedAt != nil && m.RemovedAt.Before(at) {
			at = *m.RemovedAt
		}
		if previous, ok := ends[m.UserID]; !ok || at.After(previous) {
			ends[m.UserID] = at
		}
	}
	ids := make([]uint, 0, len(ends))
	for id := range ends {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		at := ends[id]
		if e := tx.Table("account_subjects").Where("id=? AND (last_entitlement_end IS NULL OR last_entitlement_end<?)", id, at).UpdateColumn("last_entitlement_end", at).Error; e != nil {
			return e
		}
	}
	for _, row := range []any{&model.SubscriptionAutomaticRenewal{}, &model.SubscriptionToken{}, &model.SubscriptionMember{}, &model.QuotaEvent{}} {
		if e := tx.Unscoped().Where("subscription_id=?", sub.ID).Delete(row).Error; e != nil {
			return e
		}
	}
	if e := tx.Model(&model.ProtocolCredential{}).Where("subscription_id=?", sub.ID).UpdateColumn("current_scope", nil).Error; e != nil {
		return e
	}
	deleted := tx.Unscoped().Where("id=?", sub.ID).Delete(&model.Subscription{})
	if deleted.Error != nil {
		return deleted.Error
	}
	if deleted.RowsAffected != 1 {
		return errors.New("subscription cleanup lost ownership")
	}
	return tx.Create(&model.AuditLog{UserID: &sub.UserID, Actor: "system", Action: "subscription.delete", Target: fmt.Sprintf("subscription:%d", sub.ID), Detail: sub.EndReason + "; hard_deleted=true; runtime configuration removed; minimal late-accounting evidence retained"}).Error
}
