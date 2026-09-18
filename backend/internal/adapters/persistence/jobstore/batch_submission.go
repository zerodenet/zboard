package jobstore

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
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
	if err := tx.Create(&task).Error; err != nil {
		return jobs.BatchSubmissionReceipt{}, err
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
	return jobs.BatchSubmissionReceipt{BatchReceipt: jobs.BatchReceipt{
		ID: task.ID, Type: task.Type, Scope: task.Scope, Content: task.Content, Status: task.Status,
		Errors: task.Errors, Total: task.Total, Current: task.Current, IdempotencyKey: task.IdempotencyKey,
		Priority: task.Priority, ScheduledAt: task.ScheduledAt, StartedAt: task.StartedAt, FinishedAt: task.FinishedAt,
		Attempts: task.Attempts, MaxAttempts: task.MaxAttempts, LockedBy: task.LockedBy, LockedUntil: task.LockedUntil,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}}, nil
}
