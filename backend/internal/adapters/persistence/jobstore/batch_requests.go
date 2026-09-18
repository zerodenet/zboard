package jobstore

import (
	"context"
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BatchRequests struct{ DB *gorm.DB }

func (s BatchRequests) SubmitBatchRequest(ctx context.Context, actor uint, submission jobs.BatchSubmission) (receipt jobs.BatchSubmissionReceipt, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admin model.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").First(&admin).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return jobs.ErrBatchRequestPermission
			}
			return err
		}
		var persistErr error
		receipt, persistErr = PersistBatchSubmission(tx, jobs.BatchSubmissionActor{ID: admin.ID, Email: admin.Email}, submission)
		return persistErr
	})
	return receipt, err
}
