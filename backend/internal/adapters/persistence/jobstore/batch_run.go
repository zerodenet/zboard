package jobstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type BatchRun struct{ DB *gorm.DB }

func (s BatchRun) Load(ctx context.Context, id uint, token string) (jobs.LoadedBatch, error) {
	var out jobs.LoadedBatch
	err := WithBatchLeaseTask(ctx, s.DB, id, token, func(tx *gorm.DB, task model.Task) error {
		out.Task = jobs.BatchReceipt{ID: task.ID, Type: task.Type, Scope: task.Scope, Content: task.Content, Status: task.Status, Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey, Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt, Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
		out.Items = []jobs.BatchItem{}
		return tx.Model(&model.TaskItem{}).Where("task_id = ? AND status IN ?", id, []int16{0, 1, 3}).Order("id").Limit(10001).Find(&out.Items).Error
	})
	if err != nil {
		return jobs.LoadedBatch{}, err
	}
	if len(out.Items) > 10000 {
		return jobs.LoadedBatch{}, jobs.ErrBatchQuery
	}
	if out.Task.LockedUntil == nil || !out.Task.LockedUntil.After(time.Now().UTC()) {
		return jobs.LoadedBatch{}, jobs.ErrLeaseLost
	}
	return out, nil
}
