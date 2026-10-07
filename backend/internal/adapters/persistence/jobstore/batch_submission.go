package jobstore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func PersistBatchSubmission(tx *gorm.DB, actor jobs.BatchSubmissionActor, submission jobs.BatchSubmission) (jobs.BatchSubmissionReceipt, error) {
	if tx == nil || actor.ID == 0 || strings.TrimSpace(actor.Email) == "" || strings.TrimSpace(submission.Type) == "" || strings.TrimSpace(submission.Scope) == "" || strings.TrimSpace(submission.Content) == "" || strings.TrimSpace(submission.IdempotencyKey) == "" || len(submission.Targets) == 0 {
		return jobs.BatchSubmissionReceipt{}, errors.New("invalid batch submission")
	}
	if submission.MaxAttempts <= 0 {
		submission.MaxAttempts = 3
	}
	task := model.Task{
		Type: submission.Type, Scope: submission.Scope, Content: submission.Content,
		Status: 0, Total: int64(len(submission.Targets)), IdempotencyKey: submission.IdempotencyKey,
		MaxAttempts: submission.MaxAttempts, ScheduledAt: &submission.ScheduledAt,
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&task)
	if err := result.Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	var persisted model.Task
	if err := tx.Where("idempotency_key = ?", submission.IdempotencyKey).First(&persisted).Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	// MySQL may report matched rows for a no-op conflict update. Existing
	// task items also identify a replay independently of driver row counts.
	var existingItems []model.TaskItem
	if err := tx.Where("task_id = ?", persisted.ID).Order("id asc").Find(&existingItems).Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	if result.RowsAffected == 0 || persisted.ID != task.ID || len(existingItems) > 0 {
		if persisted.Type != submission.Type || persisted.Scope != submission.Scope || persisted.Content != submission.Content || persisted.Total != int64(len(submission.Targets)) {
			return jobs.BatchSubmissionReceipt{}, errors.New("idempotency key is already bound to a different task")
		}
		items := existingItems
		if len(items) != len(submission.Targets) {
			return jobs.BatchSubmissionReceipt{}, errors.New("idempotent task targets do not match")
		}
		for index, item := range items {
			if item.TargetType != submission.Targets[index].Type || item.TargetID != strconv.FormatUint(uint64(submission.Targets[index].ID), 10) {
				return jobs.BatchSubmissionReceipt{}, errors.New("idempotent task targets do not match")
			}
		}
		return batchSubmissionReceipt(persisted), nil
	}
	items := make([]model.TaskItem, 0, len(submission.Targets))
	for _, target := range submission.Targets {
		if target.ID == 0 || strings.TrimSpace(target.Type) == "" {
			return jobs.BatchSubmissionReceipt{}, errors.New("invalid batch submission target")
		}
		items = append(items, model.TaskItem{TaskID: task.ID, TargetType: target.Type, TargetID: strconv.FormatUint(uint64(target.ID), 10), Payload: "{}", Status: 0})
	}
	if err := tx.CreateInBatches(&items, 250).Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	if err := tx.Create(&model.AuditLog{UserID: &actor.ID, Actor: actor.Email, Action: "task.create", Target: fmt.Sprintf("task:%d", task.ID), Detail: fmt.Sprintf("type=%s total=%d", task.Type, task.Total)}).Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
	}
	return batchSubmissionReceipt(task), nil
}

func batchSubmissionReceipt(task model.Task) jobs.BatchSubmissionReceipt {
	return jobs.BatchSubmissionReceipt{BatchReceipt: jobs.BatchReceipt{
		ID: task.ID, Type: task.Type, Scope: task.Scope, Content: task.Content, Status: task.Status,
		Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey,
		Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt,
		Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}}
}
