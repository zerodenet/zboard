package messagingstore

import (
	"context"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Caller holds the task row lock, shared with review and execution admission.
func RequireReviewedMailRetry(tx *gorm.DB, taskID uint) error {
	var count int64
	if err := tx.Model(&model.TaskItem{}).Where("task_id = ? AND delivery_state = ?", taskID, messaging.AcceptanceUnknown).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return messaging.ErrDeliveryReviewRequired
	}
	return nil
}

type DeliveryReview struct{ DB *gorm.DB }

func (s DeliveryReview) Review(ctx context.Context, actor, taskID, itemID uint, in messaging.DeliveryReviewInput) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := templateAdministrator(tx, actor)
		if err != nil {
			return err
		}
		var task model.Task
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND type = ?", taskID, "email").Limit(1).Find(&task)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected != 1 {
			return messaging.ErrDeliveryReviewConflict
		}
		now := time.Now().UTC()
		if task.Status == 1 && (task.LockedUntil == nil || task.LockedUntil.After(now)) {
			return messaging.ErrDeliveryReviewConflict
		}
		var item model.TaskItem
		found = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND task_id = ? AND delivery_state = ? AND attempts = ?", itemID, taskID, messaging.AcceptanceUnknown, in.ExpectedAttempt).Limit(1).Find(&item)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected != 1 {
			return messaging.ErrDeliveryReviewConflict
		}
		status := int16(3)
		if in.Acceptance == messaging.Accepted {
			status = 2
		}
		if err := tx.Model(&item).Updates(map[string]any{"status": status, "delivery_state": in.Acceptance, "error": "", "finished_at": now}).Error; err != nil {
			return err
		}
		var remaining, handled int64
		if err := tx.Model(&model.TaskItem{}).Where("task_id = ? AND status <> ?", taskID, 2).Count(&remaining).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.TaskItem{}).Where("task_id = ? AND status IN ?", taskID, []int16{2, 3}).Count(&handled).Error; err != nil {
			return err
		}
		updates := map[string]any{"status": int16(3), "current": handled, "locked_by": "", "locked_until": nil, "scheduled_at": nil, "finished_at": now, "errors": "邮件结果已核验；尚未完成的目标可按任务重试"}
		if remaining == 0 {
			updates["status"] = int16(2)
			updates["errors"] = ""
		}
		if err := tx.Model(&task).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "message.delivery.review", Target: fmt.Sprintf("task:%d/item:%d", taskID, itemID), Detail: fmt.Sprintf("attempt=%d acceptance=%s reason=%s", in.ExpectedAttempt, in.Acceptance, in.Reason)}).Error
	})
}
