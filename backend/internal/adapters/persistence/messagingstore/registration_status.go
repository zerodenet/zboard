package messagingstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type RegistrationStatus struct{ DB *gorm.DB }

func (s RegistrationStatus) Pending(ctx context.Context, actor uint, limit, offset int) (messaging.RegistrationEventStatus, error) {
	out := messaging.RegistrationEventStatus{Items: []messaging.PendingRegistrationEvent{}}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := templateAdministrator(tx, actor); err != nil {
			return err
		}
		if err := tx.Model(&model.AccountRegistrationEvent{}).Where("processed_at IS NULL").Count(&out.Pending).Error; err != nil {
			return err
		}
		var oldest []model.AccountRegistrationEvent
		if err := tx.Where("processed_at IS NULL").Order("occurred_at, account_id").Limit(1).Find(&oldest).Error; err != nil {
			return err
		}
		if len(oldest) > 0 {
			out.OldestAt = &oldest[0].OccurredAt
		}
		var rows []model.AccountRegistrationEvent
		if err := tx.Where("processed_at IS NULL").Order("account_id").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			out.Items = append(out.Items, messaging.PendingRegistrationEvent{AccountID: row.AccountID, OccurredAt: row.OccurredAt})
		}
		return nil
	})
	if err != nil {
		return messaging.RegistrationEventStatus{}, err
	}
	return out, nil
}
