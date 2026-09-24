package entitlementstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const trafficResetBatchSize = 200

type TrafficReset struct {
	DB      *gorm.DB
	Issuer  entitlements.CredentialIssuer
	Publish func(*gorm.DB, uint, uint) error
}

func (s TrafficReset) RunDue(ctx context.Context, now time.Time) error {
	var failures []error
	var lastID uint
	for {
		var ids []uint
		if err := s.DB.WithContext(ctx).Model(&model.Subscription{}).
			Where("id > ? AND reset_policy BETWEEN 1 AND 4 AND (next_reset_at <= ? OR next_reset_at IS NULL) AND end_at > ? AND status IN ?",
				lastID, now.UTC(), now.UTC(), []string{"active", "expired"}).
			Order("id asc").Limit(trafficResetBatchSize).Pluck("id", &ids).Error; err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, id := range ids {
			if _, err := s.ResetDue(ctx, id, now.UTC()); err != nil {
				failures = append(failures, fmt.Errorf("subscription %d: %w", id, err))
			}
		}
		if len(ids) < trafficResetBatchSize {
			break
		}
		lastID = ids[len(ids)-1]
	}
	return errors.Join(failures...)
}

func (s TrafficReset) ResetDue(ctx context.Context, id uint, now time.Time) (bool, error) {
	changed := false
	err := RunCredentialTransaction(ctx, s.DB, func(tx *gorm.DB) error {
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, id).Error; err != nil {
			return err
		}
		if sub.ResetPolicy < 1 || sub.ResetPolicy > 4 || (sub.Status != "active" && sub.Status != "expired") ||
			!sub.EndAt.After(now) {
			return nil
		}
		if sub.NextResetAt == nil {
			sub.NextResetAt = entitlements.NextTrafficReset(sub.StartAt, sub.ResetPolicy)
			if sub.NextResetAt == nil {
				return errors.New("unable to schedule subscription traffic reset")
			}
			if sub.NextResetAt.After(now) {
				return tx.Model(&sub).Update("next_reset_at", sub.NextResetAt).Error
			}
		}
		if sub.NextResetAt.After(now) {
			return nil
		}
		wasInactive := sub.Status != "active"
		if _, err := ApplyDueTrafficReset(tx, &sub, now); err != nil {
			return err
		}
		if sub.FlowTotal > sub.FlowUsed {
			sub.Status = "active"
		} else {
			sub.Status = "expired"
		}
		sub.UpdatedAt = now
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		if wasInactive && sub.Status == "active" {
			if _, err := EnsureCredentials(tx, sub, s.Issuer); err != nil {
				return err
			}
		}
		if sub.Status == "expired" {
			if err := ExpireSubscriptionCredentials(tx, sub.ID, now); err != nil {
				return err
			}
		}
		if err := s.Publish(tx, sub.ID, 0); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed && err == nil, err
}

// The caller locks the subscription row. It also owns saving the changed
// subscription and restoring credentials/publication after this quota change.
func ApplyDueTrafficReset(tx *gorm.DB, sub *model.Subscription, now time.Time) (bool, error) {
	if sub == nil || sub.ResetPolicy < 1 || sub.ResetPolicy > 4 || sub.NextResetAt == nil || sub.NextResetAt.After(now) {
		return false, nil
	}
	if sub.ResetQuotaBytes < 0 || sub.FlowUsed < 0 || sub.FlowUsed > math.MaxInt64-sub.ResetQuotaBytes {
		return false, errors.New("invalid subscription reset quota or cumulative usage")
	}
	next := entitlements.NextTrafficResetAfter(sub.StartAt, sub.ResetPolicy, now)
	if next == nil || !next.After(now) {
		return false, errors.New("invalid next traffic reset time")
	}
	scheduledAt := sub.NextResetAt.UTC()
	cycleStart := scheduledAt
	if !next.After(cycleStart) {
		return false, errors.New("invalid traffic reset schedule")
	}
	for {
		following := entitlements.NextTrafficResetAfter(sub.StartAt, sub.ResetPolicy, cycleStart)
		if following == nil || !following.After(cycleStart) {
			return false, errors.New("invalid traffic reset schedule")
		}
		if following.After(now) {
			break
		}
		cycleStart = *following
	}
	var usedSince int64
	if err := tx.Model(&model.TrafficRecord{}).
		Select("COALESCE(SUM(used_bytes), 0)").
		Where("subscription_id = ? AND created_at >= ? AND created_at <= ?", sub.ID, cycleStart, now).
		Scan(&usedSince).Error; err != nil {
		return false, err
	}
	var grantsSince int64
	if err := tx.Model(&model.QuotaEvent{}).
		Select("COALESCE(SUM(delta_bytes), 0)").
		Where("subscription_id = ? AND event_type IN ? AND created_at >= ? AND created_at <= ?",
			sub.ID, []string{"traffic_pack", "renewal", "upgrade", "task_adjustment"}, cycleStart, now).
		Scan(&grantsSince).Error; err != nil {
		return false, err
	}
	if usedSince < 0 || usedSince > sub.FlowUsed ||
		grantsSince > math.MaxInt64-sub.ResetQuotaBytes {
		return false, errors.New("invalid current-cycle quota ledger")
	}
	quota := max(int64(0), sub.ResetQuotaBytes+grantsSince)
	baseline := sub.FlowUsed - usedSince
	if baseline > math.MaxInt64-max(quota, usedSince) {
		return false, errors.New("current-cycle quota overflows")
	}
	before := sub.FlowTotal - sub.FlowUsed
	sub.FlowTotal = baseline + max(quota, usedSince)
	sub.CycleStartUsed = baseline
	sub.NextResetAt = next
	after := sub.FlowTotal - sub.FlowUsed
	detail, _ := json.Marshal(map[string]any{"scheduled_at": scheduledAt.Format(time.RFC3339Nano), "cycle_start_at": cycleStart.Format(time.RFC3339Nano), "base_quota_bytes": sub.ResetQuotaBytes})
	if err := tx.Create(&model.QuotaEvent{
		SubscriptionID: sub.ID, EventType: "reset", DeltaBytes: after - before,
		BalanceBefore: before, BalanceAfter: after,
		ReferenceType: "traffic_reset", ReferenceID: scheduledAt.Format(time.RFC3339Nano), Detail: string(detail),
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}
