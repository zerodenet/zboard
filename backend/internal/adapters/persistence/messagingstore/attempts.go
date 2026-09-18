package messagingstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

// StartMailAttempt must run in the transaction that holds the task lease and
// increments the item attempt. Read the persisted counter, not a GORM expression.
func StartMailAttempt(tx *gorm.DB, taskID, itemID uint, token string, now time.Time) (int, error) {
	var item model.TaskItem
	if err := tx.Where("id = ? AND task_id = ? AND status = ?", itemID, taskID, 1).First(&item).Error; err != nil {
		return 0, err
	}
	row := model.MailDeliveryAttempt{TaskID: taskID, ItemID: itemID, Attempt: item.Attempts, LeaseToken: token, Acceptance: string(messaging.AcceptanceUnknown), StartedAt: now}
	return item.Attempts, tx.Create(&row).Error
}

// FinishMailAttempt shares the lease-fenced item completion transaction.
func FinishMailAttempt(tx *gorm.DB, taskID, itemID uint, attempt int, token string, acceptance messaging.Acceptance, now time.Time) error {
	result := tx.Model(&model.MailDeliveryAttempt{}).Where("task_id = ? AND item_id = ? AND attempt = ? AND lease_token = ? AND finished_at IS NULL", taskID, itemID, attempt, token).Updates(map[string]any{"acceptance": string(acceptance), "finished_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return jobs.ErrLeaseLost
	}
	return nil
}

func PruneMailAttempts(tx *gorm.DB, taskIDs []uint) error {
	return tx.Where("task_id IN ?", taskIDs).Delete(&model.MailDeliveryAttempt{}).Error
}
