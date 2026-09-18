package messagingstore

import (
	"context"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProcessRegistrationEvents converts a bounded set of committed identity events
// into durable delivery intents. The event checkpoint and task commit together.
func (s RegistrationWelcome) ProcessRegistrationEvents(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, errors.New("invalid registration event batch")
	}
	var events []model.AccountRegistrationEvent
	if err := s.DB.WithContext(ctx).Where("processed_at IS NULL").Order("account_id").Limit(limit).Find(&events).Error; err != nil {
		return 0, err
	}
	processed := 0
	for _, candidate := range events {
		didProcess := false
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var event model.AccountRegistrationEvent
			found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ? AND processed_at IS NULL", candidate.AccountID).Limit(1).Find(&event)
			if found.Error != nil {
				return found.Error
			}
			if found.RowsAffected == 0 {
				return nil
			}
			child := RegistrationWelcome{DB: tx, Cipher: s.Cipher}
			receipt, err := child.EnqueueRegisteredAccount(ctx, event.AccountID)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			updates := map[string]any{"processed_at": now}
			if receipt.TaskID != 0 {
				updates["task_id"] = receipt.TaskID
			}
			if err := tx.Model(&event).Updates(updates).Error; err != nil {
				return err
			}
			didProcess = true
			return nil
		})
		if err != nil {
			return processed, err
		}
		if didProcess {
			processed++
		}
	}
	return processed, nil
}
