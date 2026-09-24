package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	trafficResetPollInterval = time.Minute
	trafficResetBatchSize    = 200
)

type trafficResetRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var trafficResetWorkerRegistry sync.Map

func (h *handlers) StartTrafficResetWorker() {
	if h == nil || h.db == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &trafficResetRuntime{cancel: cancel, done: make(chan struct{})}
	if _, loaded := trafficResetWorkerRegistry.LoadOrStore(h, runtime); loaded {
		cancel()
		return
	}
	go func() {
		defer close(runtime.done)
		h.runTrafficResetWorker(ctx)
	}()
}

func (h *handlers) runTrafficResetWorker(ctx context.Context) {
	run := func() {
		if err := h.runDueTrafficResets(time.Now().UTC()); err != nil && ctx.Err() == nil {
			log.Printf("subscription traffic reset failed: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(trafficResetPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (h *handlers) CloseTrafficResetWorker() {
	value, ok := trafficResetWorkerRegistry.LoadAndDelete(h)
	if !ok {
		return
	}
	runtime := value.(*trafficResetRuntime)
	runtime.cancel()
	<-runtime.done
}

func (h *handlers) runDueTrafficResets(now time.Time) error {
	if h.backgroundWorkPaused() {
		return nil
	}
	var failures []error
	var lastID uint
	for {
		var ids []uint
		if err := h.db.Model(&model.Subscription{}).
			Where("id > ? AND reset_policy BETWEEN 1 AND 4 AND (next_reset_at <= ? OR next_reset_at IS NULL) AND end_at > ? AND status IN ?",
				lastID, now.UTC(), now.UTC(), []string{subStatusActive, subStatusExpired}).
			Order("id asc").Limit(trafficResetBatchSize).Pluck("id", &ids).Error; err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, id := range ids {
			changed, err := h.resetDueSubscription(id, now.UTC())
			if err != nil {
				failures = append(failures, fmt.Errorf("subscription %d: %w", id, err))
			} else if changed {
				h.publishScheduler().signal()
			}
		}
		if len(ids) < trafficResetBatchSize {
			break
		}
		lastID = ids[len(ids)-1]
	}
	return errors.Join(failures...)
}

func (h *handlers) resetDueSubscription(id uint, now time.Time) (bool, error) {
	changed := false
	err := h.runProtocolCredentialTransaction(func(tx *gorm.DB) error {
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, id).Error; err != nil {
			return err
		}
		if sub.ResetPolicy < 1 || sub.ResetPolicy > 4 || (sub.Status != subStatusActive && sub.Status != subStatusExpired) ||
			!sub.EndAt.After(now) {
			return nil
		}
		if sub.NextResetAt == nil {
			sub.NextResetAt = nextTrafficReset(sub.StartAt, sub.ResetPolicy)
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
		wasInactive := sub.Status != subStatusActive
		if _, err := applyDueTrafficReset(tx, &sub, now); err != nil {
			return err
		}
		if sub.FlowTotal > sub.FlowUsed {
			sub.Status = subStatusActive
		} else {
			sub.Status = subStatusExpired
		}
		sub.UpdatedAt = now
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		if wasInactive && sub.Status == subStatusActive {
			if _, err := h.ensureSubscriptionCredentials(tx, sub); err != nil {
				return err
			}
		}
		if sub.Status == subStatusExpired {
			credentials := tx.Model(&model.ProtocolCredential{}).
				Where("subscription_id = ? AND status IN ?", sub.ID,
					[]string{protocolCredentialStatusActive, protocolCredentialStatusPrepared})
			if err := persistCredentialRevocation(tx, credentials, subStatusExpired, now); err != nil {
				return err
			}
		}
		if err := enqueueSubscriptionConfigPublishes(tx, sub.ID, 0); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed && err == nil, err
}

// The caller locks the subscription row. It also owns saving the changed
// subscription and restoring credentials/publication after this quota change.
func applyDueTrafficReset(tx *gorm.DB, sub *model.Subscription, now time.Time) (bool, error) {
	if sub == nil || sub.ResetPolicy < 1 || sub.ResetPolicy > 4 || sub.NextResetAt == nil || sub.NextResetAt.After(now) {
		return false, nil
	}
	if sub.ResetQuotaBytes < 0 || sub.FlowUsed < 0 || sub.FlowUsed > math.MaxInt64-sub.ResetQuotaBytes {
		return false, errors.New("invalid subscription reset quota or cumulative usage")
	}
	next := nextTrafficResetAfter(sub.StartAt, sub.ResetPolicy, now)
	if next == nil || !next.After(now) {
		return false, errors.New("invalid next traffic reset time")
	}
	scheduledAt := sub.NextResetAt.UTC()
	cycleStart := scheduledAt
	if !next.After(cycleStart) {
		return false, errors.New("invalid traffic reset schedule")
	}
	for {
		following := nextTrafficResetAfter(sub.StartAt, sub.ResetPolicy, cycleStart)
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
