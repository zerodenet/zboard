package observabilitystore

import (
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// Retain a terminal parent across invocations until bounded descendant work has
// finished. A task with a million items cannot create a million-row transaction.
func pruneTaskHistoryChildren(tx *gorm.DB, parents []uint, limit int, count *int64) error {
	var attempts []uint
	if err := tx.Model(&model.MailDeliveryAttempt{}).Where("task_id IN ?", parents).Order("task_id,id").Limit(limit).Pluck("id", &attempts).Error; err != nil {
		return err
	}
	if len(attempts) > 0 {
		if err := tx.Where("id IN ?", attempts).Delete(&model.MailDeliveryAttempt{}).Error; err != nil {
			return err
		}
	}
	var items []uint
	if err := tx.Model(&model.TaskItem{}).Where("task_id IN ?", parents).Where("NOT EXISTS (?)", tx.Model(&model.MailDeliveryAttempt{}).Select("1").Where("mail_delivery_attempts.item_id = task_items.id")).Order("task_id,id").Limit(limit).Pluck("id", &items).Error; err != nil {
		return err
	}
	if len(items) > 0 {
		if err := tx.Where("id IN ?", items).Delete(&model.TaskItem{}).Error; err != nil {
			return err
		}
	}
	res := tx.Where("id IN ?", parents).
		Where("NOT EXISTS (?)", tx.Model(&model.TaskItem{}).Select("1").Where("task_items.task_id = tasks.id")).
		Where("NOT EXISTS (?)", tx.Model(&model.MailDeliveryAttempt{}).Select("1").Where("mail_delivery_attempts.task_id = tasks.id")).Delete(&model.Task{})
	*count = res.RowsAffected
	return res.Error
}
