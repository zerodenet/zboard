package observabilitystore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/observability"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Audit struct{ DB *gorm.DB }

func (s Audit) RecordAdminAudit(ctx context.Context, actor uint, event observability.AuditEvent) error {
	if s.DB == nil {
		return observability.ErrAuditUnavailable
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admin model.User
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").
			First(&admin).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return observability.ErrAuditPermission
		}
		if err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{
			UserID: &admin.ID, Actor: admin.Email, Action: event.Action,
			Target: event.Target, Detail: event.Detail,
		}).Error
	})
}
