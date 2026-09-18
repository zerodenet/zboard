package entitlementstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
	"strings"
	"time"
)

type QuotaExecution struct {
	DB      *gorm.DB
	Publish func(*gorm.DB, uint, uint) error
}

func (s QuotaExecution) Execute(ctx context.Context, claim entitlements.QuotaExecutionClaim) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND type = ? AND status = 1 AND locked_by = ? AND locked_until > ?", claim.TaskID, "quota", claim.Token, time.Now().UTC()).First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return jobs.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		var item model.TaskItem
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ? AND target_type = ? AND status = 1", claim.ItemID, claim.TaskID, "subscription").First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return jobs.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(item.TargetID, 10, 64)
		if err != nil || id == 0 {
			return entitlements.ErrQuotaRequestInvalid
		}
		var content entitlements.QuotaAdjustment
		if err := json.Unmarshal([]byte(task.Content), &content); err != nil {
			return err
		}
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&sub, uint(id)).Error; err != nil {
			return err
		}
		// Serialize against other adjustments before checking replay; a crash between
		// this commit and task progress persistence must not change quota twice.
		var existing int64
		if err := tx.Model(&model.QuotaEvent{}).Where("subscription_id = ? AND event_type = ? AND reference_type = ? AND reference_id = ?", sub.ID, "task_adjustment", "task_item", strconv.FormatUint(uint64(item.ID), 10)).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		next, delta, err := entitlements.AdjustQuota(sub.FlowTotal, sub.FlowUsed, content)
		if err != nil {
			return err
		}
		before := sub.FlowTotal - sub.FlowUsed
		updates := map[string]any{"flow_total": next}
		if sub.EndAt.After(time.Now().UTC()) && next > sub.FlowUsed {
			updates["status"] = "active"
		}
		if err := tx.Model(&sub).Updates(updates).Error; err != nil {
			return err
		}
		if s.Publish == nil {
			return errors.New("quota publication unavailable")
		}
		if err := s.Publish(tx, sub.ID, 0); err != nil {
			return err
		}
		detail, err := json.Marshal(map[string]any{"reason": strings.TrimSpace(content.Reason), "task_id": task.ID})
		if err != nil {
			return err
		}
		if err := tx.Create(&model.QuotaEvent{SubscriptionID: sub.ID, EventType: "task_adjustment", DeltaBytes: delta, BalanceBefore: before, BalanceAfter: next - sub.FlowUsed, ReferenceType: "task_item", ReferenceID: strconv.FormatUint(uint64(item.ID), 10), Detail: string(detail)}).Error; err != nil {
			return err
		}
		if task.LockedUntil == nil || !task.LockedUntil.After(time.Now().UTC()) {
			return jobs.ErrLeaseLost
		}
		return nil
	})
}
