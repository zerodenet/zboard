package jobstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type BatchControl struct {
	DB             *gorm.DB
	MailRetryGuard func(*gorm.DB, uint) error
}

func (s BatchControl) Queue(ctx context.Context, actor, id uint) error {
	if actor == 0 {
		return jobs.ErrBatchPermission
	}
	return s.queue(ctx, actor, id)
}

// QueueInternal is an application-only scheduling bridge for already persisted
// batches. It is not an externally callable user/plugin authority bypass.
func (s BatchControl) QueueInternal(ctx context.Context, id uint) error { return s.queue(ctx, 0, id) }
func (s BatchControl) check(tx *gorm.DB, task model.Task) error {
	if task.Type == "email" {
		if s.MailRetryGuard == nil {
			return errors.New("mail retry guard unavailable")
		}
		if err := s.MailRetryGuard(tx, task.ID); err != nil {
			return err
		}
	}
	if task.Type == "database_migration" {
		return errors.New("database migrations are managed by the maintenance workflow")
	}
	if task.Status == 2 {
		return errors.New("completed task cannot run again")
	}
	if task.Attempts >= task.MaxAttempts {
		return errors.New("task has reached max_attempts")
	}
	return nil
}
func batchActor(tx *gorm.DB, actor uint) (model.User, error) {
	var user model.User
	if actor == 0 {
		return user, nil
	}
	if err := batchReader(tx, actor); err != nil {
		return user, err
	}
	err := tx.Select("id", "email").First(&user, actor).Error
	return user, err
}
func batchRunAudit(tx *gorm.DB, user model.User, id uint) error {
	if user.ID == 0 {
		return nil
	}
	return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "task.run", Target: fmt.Sprintf("task:%d", id), Detail: "queued"}).Error
}
func (s BatchControl) queue(ctx context.Context, actor, id uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := batchActor(tx, actor)
		if err != nil {
			return err
		}
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		if err := s.check(tx, task); err != nil {
			return err
		}
		if task.Status == 1 {
			return errors.New("task is running or awaiting interrupted-execution recovery")
		}
		if err := batchRunAudit(tx, user, id); err != nil {
			return err
		}
		return tx.Model(&task).Updates(map[string]any{"status": 0, "scheduled_at": time.Now().UTC(), "finished_at": nil}).Error
	})
}

// Claim is used only by the admitted batch runtime; it does not add workers.
func (s BatchControl) Claim(ctx context.Context, actor, id uint, queuedOnly bool) (string, error) {
	token := uuid.NewString()
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := batchActor(tx, actor)
		if err != nil {
			return err
		}
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, id).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := s.check(tx, task); err != nil {
			return err
		}
		if queuedOnly && (task.Status != 0 || task.ScheduledAt == nil || task.ScheduledAt.After(now)) {
			return errors.New("task is no longer queued")
		}
		if task.Status == 1 && (task.LockedUntil == nil || task.LockedUntil.After(now)) {
			return errors.New("task is already running")
		}
		if err := batchRunAudit(tx, user, id); err != nil {
			return err
		}
		var completed int64
		if err := tx.Model(&model.TaskItem{}).Where("task_id = ? AND status = 2", task.ID).Count(&completed).Error; err != nil {
			return err
		}
		return tx.Model(&task).Updates(map[string]any{"status": 1, "errors": "", "started_at": now, "finished_at": nil, "attempts": gorm.Expr("attempts + 1"), "current": completed, "locked_by": token, "locked_until": now.Add(30 * time.Minute)}).Error
	})
	if err != nil {
		return "", err
	}
	return token, nil
}
