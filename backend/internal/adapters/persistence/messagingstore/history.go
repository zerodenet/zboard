package messagingstore

import (
	"context"
	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type DeliveryHistory struct{ DB *gorm.DB }

func (s DeliveryHistory) List(ctx context.Context, actor, taskID, itemID uint, limit, offset int) (messaging.DeliveryHistoryPage, error) {
	out := messaging.DeliveryHistoryPage{Items: []messaging.DeliveryAttempt{}, Offset: offset, Limit: limit}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := templateAdministrator(tx, actor); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.TaskItem{}).Joins("JOIN tasks ON tasks.id = task_items.task_id").Where("task_items.id = ? AND tasks.id = ? AND tasks.type = ?", itemID, taskID, "email").Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return messaging.ErrDeliveryNotFound
		}
		query := tx.Model(&model.MailDeliveryAttempt{}).Where("task_id = ? AND item_id = ?", taskID, itemID)
		if err := query.Count(&out.Total).Error; err != nil {
			return err
		}
		// Do not expose execution lease tokens or message contents to the read API.
		return query.Select("id, attempt, acceptance, started_at, finished_at").Order("attempt DESC").Limit(limit).Offset(offset).Scan(&out.Items).Error
	})
	if err != nil {
		return messaging.DeliveryHistoryPage{}, err
	}
	return out, nil
}
