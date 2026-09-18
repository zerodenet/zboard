package messagingstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

func BeginBatchItem(tx *gorm.DB, task model.Task, item model.TaskItem, now time.Time) error {
	if task.Type != "email" {
		return nil
	}
	if err := tx.Model(&item).Update("delivery_state", messaging.AcceptanceUnknown).Error; err != nil {
		return err
	}
	_, err := StartMailAttempt(tx, task.ID, item.ID, task.LockedBy, now)
	return err
}
func CompleteBatchItem(tx *gorm.DB, task model.Task, item model.TaskItem, outcome jobs.BatchItemOutcome, now time.Time) error {
	if task.Type != "email" || outcome.Panicked {
		return nil
	}
	acceptance := messaging.AcceptanceFor(outcome.Cause)
	if err := FinishMailAttempt(tx, task.ID, item.ID, item.Attempts, task.LockedBy, acceptance, now); err != nil {
		return err
	}
	return tx.Model(&item).Update("delivery_state", acceptance).Error
}
