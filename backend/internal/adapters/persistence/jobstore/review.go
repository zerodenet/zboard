package jobstore

import (
	"context"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) RecordVerifiedOutcome(ctx context.Context, actor jobs.Reviewer, in jobs.Review) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		q := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ? AND is_admin = ?", actor.AccountID, "active", true).Limit(1).Find(&user)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return jobs.ErrPermission
		}
		if err := New(tx).Resolve(ctx, in.RunID, in.Outcome); err != nil {
			return err
		}
		return tx.Create(&model.AuditLog{UserID: &user.ID, Actor: user.Email, Action: "job.resolve", Target: "job:" + in.RunID, Detail: string(in.Outcome) + ": " + in.Reason}).Error
	})
}
