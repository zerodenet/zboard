package jobstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type BatchLifecycle struct{ DB *gorm.DB }

func (s BatchLifecycle) Pending(ctx context.Context) (bool, error) {
	now := time.Now().UTC()
	var row struct{ Present int }
	err := s.DB.WithContext(ctx).Model(&model.Task{}).
		Where("type <> ?", "database_migration").
		Where("(status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ? AND attempts < max_attempts) OR (status = ? AND (locked_until IS NULL OR locked_until <= ?))", 0, now, 1, now).
		Select("1 AS present").Limit(1).Scan(&row).Error
	return row.Present == 1, err
}

func (s BatchLifecycle) Ready(ctx context.Context, limit int) ([]uint, error) {
	now := time.Now().UTC()
	// Recovery never silently requeues uncertain external effects.
	if err := s.DB.WithContext(ctx).Model(&model.Task{}).Where("type <> ? AND status = 1 AND (locked_until IS NULL OR locked_until <= ?)", "database_migration", now).Updates(map[string]any{"status": 3, "errors": "执行租约已失效；外部操作结果待核验，请核验后重试。", "finished_at": now, "locked_by": "", "locked_until": nil}).Error; err != nil {
		return nil, err
	}
	ids := []uint{}
	err := s.DB.WithContext(ctx).Model(&model.Task{}).Where("type <> ? AND status = 0 AND scheduled_at IS NOT NULL AND scheduled_at <= ? AND attempts < max_attempts", "database_migration", now).Order("priority DESC, scheduled_at ASC, id ASC").Limit(limit).Pluck("id", &ids).Error
	return ids, err
}
func (s BatchLifecycle) Renew(ctx context.Context, id uint, token string) error {
	now := time.Now().UTC()
	res := s.DB.WithContext(ctx).Model(&model.Task{}).Where("id = ? AND status = 1 AND locked_by = ? AND locked_until > ?", id, token, now).Update("locked_until", now.Add(30*time.Minute))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return jobs.ErrLeaseLost
	}
	return nil
}

// WithBatchLease is a temporary transaction bridge for per-item execution while
// those records are moved to their owning capability. Never export it over HTTP.
func WithBatchLease(ctx context.Context, db *gorm.DB, id uint, token string, write func(*gorm.DB) error) error {
	return WithBatchLeaseTask(ctx, db, id, token, func(tx *gorm.DB, _ model.Task) error { return write(tx) })
}

// WithBatchLeaseTask exposes the task row already locked for the lease so
// persistence adapters do not issue a second SELECT for the same task.
func WithBatchLeaseTask(ctx context.Context, db *gorm.DB, id uint, token string, write func(*gorm.DB, model.Task) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = 1 AND locked_by = ? AND locked_until > ?", id, token, time.Now().UTC()).First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return jobs.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		return write(tx, task)
	})
}
func (s BatchLifecycle) Finish(ctx context.Context, id uint, token string, failures []string) error {
	return WithBatchLease(ctx, s.DB, id, token, func(tx *gorm.DB) error {
		status := int16(2)
		if len(failures) > 0 {
			status = 3
		} else {
			var remaining int64
			if err := tx.Model(&model.TaskItem{}).Where("task_id = ? AND status <> 2", id).Count(&remaining).Error; err != nil {
				return err
			}
			if remaining > 0 {
				status = 3
				failures = []string{"任务仍有未完成目标，请检查执行结果。"}
			}
		}
		return tx.Model(&model.Task{}).Where("id = ?", id).Updates(map[string]any{"status": status, "errors": strings.Join(failures, "\n"), "finished_at": time.Now().UTC(), "locked_by": "", "locked_until": nil}).Error
	})
}
