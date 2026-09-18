package messagingstore

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EmailExecution struct{ DB *gorm.DB }

func emailClaim(tx *gorm.DB, claim messaging.EmailExecutionClaim) (model.Task, model.TaskItem, error) {
	var task model.Task
	var item model.TaskItem
	result := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND type = ? AND status = ? AND locked_by = ? AND locked_until > ?", claim.TaskID, "email", 1, claim.Token, time.Now().UTC()).Limit(1).Find(&task)
	if result.Error != nil {
		return task, item, result.Error
	}
	if result.RowsAffected != 1 {
		return task, item, jobs.ErrLeaseLost
	}
	result = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND task_id = ? AND target_type = ? AND status = ?", claim.ItemID, task.ID, "user", 1).Limit(1).Find(&item)
	if result.Error != nil {
		return task, item, result.Error
	}
	if result.RowsAffected != 1 {
		return task, item, jobs.ErrLeaseLost
	}
	return task, item, nil
}
func (s EmailExecution) Prepare(ctx context.Context, claim messaging.EmailExecutionClaim) (messaging.EmailTaskData, error) {
	var out messaging.EmailTaskData
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, item, err := emailClaim(tx, claim)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(item.TargetID, 10, 64)
		if err != nil || id == 0 {
			return messaging.ErrInvalidMessage
		}
		var user model.User
		if err := tx.Where("id = ? AND status = ?", id, "active").First(&user).Error; err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(task.Content), &out.Content); err != nil {
			return messaging.ErrInvalidMessage
		}
		out.Recipient = messaging.EmailRecipient{Email: user.Email, AccountName: user.AccountName, RegisteredAt: user.CreatedAt}
		out.LeaseUntil = *task.LockedUntil
		return nil
	})
	if err != nil {
		return messaging.EmailTaskData{}, err
	}
	return out, nil
}
func (s EmailExecution) Check(ctx context.Context, claim messaging.EmailExecutionClaim) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { _, _, err := emailClaim(tx, claim); return err })
}
