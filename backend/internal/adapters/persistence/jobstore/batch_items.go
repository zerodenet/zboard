package jobstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type BatchItemHooks struct {
	Begin    func(*gorm.DB, model.Task, model.TaskItem, time.Time) error
	Complete func(*gorm.DB, model.Task, model.TaskItem, jobs.BatchItemOutcome, time.Time) error
}
type BatchItems struct {
	DB    *gorm.DB
	Hooks BatchItemHooks
}

func (s BatchItems) Begin(ctx context.Context, claim jobs.BatchItemClaim) (jobs.BatchAttempt, error) {
	var out jobs.BatchAttempt
	err := WithBatchLeaseTask(ctx, s.DB, claim.TaskID, claim.Token, func(tx *gorm.DB, task model.Task) error {
		var item model.TaskItem
		err := tx.Where("id = ? AND task_id = ? AND status IN ?", claim.ItemID, claim.TaskID, []int16{0, 1, 3}).First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return jobs.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&item).Updates(map[string]any{"status": 1, "attempts": gorm.Expr("attempts + 1"), "error": "", "started_at": now, "finished_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.First(&item, item.ID).Error; err != nil {
			return err
		}
		if s.Hooks.Begin != nil {
			if err := s.Hooks.Begin(tx, task, item, now); err != nil {
				return err
			}
		}
		out.Task = jobs.BatchReceipt{ID: task.ID, Type: task.Type, Scope: task.Scope, Content: task.Content, Status: task.Status, Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey, Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
		out.Item = jobs.BatchItem{DeliveryState: item.DeliveryState, ID: item.ID, TaskID: item.TaskID, TargetType: item.TargetType, TargetID: item.TargetID, Payload: item.Payload, Status: item.Status, Attempts: item.Attempts, Error: item.Error, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
		return nil
	})
	if err != nil {
		return jobs.BatchAttempt{}, err
	}
	return out, nil
}
func (s BatchItems) Complete(ctx context.Context, claim jobs.BatchItemClaim, attempt int, outcome jobs.BatchItemOutcome) error {
	return WithBatchLeaseTask(ctx, s.DB, claim.TaskID, claim.Token, func(tx *gorm.DB, task model.Task) error {
		var item model.TaskItem
		err := tx.Where("id = ? AND task_id = ? AND status = 1 AND attempts = ?", claim.ItemID, claim.TaskID, attempt).First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return jobs.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		status := int16(2)
		if outcome.Cause != nil || outcome.Panicked {
			status = 3
		}
		if s.Hooks.Complete != nil {
			if err := s.Hooks.Complete(tx, task, item, outcome, now); err != nil {
				return err
			}
		}
		if err := tx.Model(&item).Updates(map[string]any{"status": status, "error": outcome.Message, "finished_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&task).Update("current", gorm.Expr("current + 1")).Error
	})
}
