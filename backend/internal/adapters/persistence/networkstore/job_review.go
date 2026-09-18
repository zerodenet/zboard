package networkstore

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/platformstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JobReviews struct{ DB *gorm.DB }

func (s JobReviews) RecordVerifiedOutcome(ctx context.Context, actor jobs.Reviewer, in jobs.Review) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := jobstore.New(tx).RecordVerifiedOutcome(ctx, actor, in); err != nil {
			return err
		}
		if err := verifyNativeOutcome(tx, in); err != nil {
			return err
		}
		return platformstore.ApplyMigrationReview(tx, in)
	})
}

// A task review cannot invent domain results. In particular, clearing a Run
// while its certificate/DNS operation is still running splits authoritative
// state and may permit overlapping external actions. The transaction rolls
// the Run resolution back unless the domain has the same terminal outcome.
func verifyNativeOutcome(tx *gorm.DB, in jobs.Review) error {
	var run jobstore.Record
	if err := tx.First(&run, "id = ?", in.RunID).Error; err != nil {
		return err
	}
	if run.Owner != "system" || (run.Handler != "certificate_operation" && run.Handler != "dns_operation") {
		return nil
	}
	var input struct {
		OperationID uint `json:"operation_id"`
	}
	if json.Unmarshal([]byte(run.Payload), &input) != nil || input.OperationID == 0 {
		return jobs.ErrOutcomeUnverified
	}
	if run.Key != fmt.Sprintf("%s:%d", run.Handler, input.OperationID) {
		return jobs.ErrOutcomeUnverified
	}
	var status string
	if run.Handler == "certificate_operation" {
		var op model.CertificateOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&op, input.OperationID).Error; err != nil {
			return jobs.ErrOutcomeUnverified
		}
		if run.Resource != fmt.Sprintf("certificate:%d", op.ManagedCertificateID) {
			return jobs.ErrOutcomeUnverified
		}
		status = op.Status
	} else {
		var op model.ProviderOperation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND resource_type = ?", input.OperationID, "dns_record").First(&op).Error; err != nil {
			return jobs.ErrOutcomeUnverified
		}
		if run.Resource != fmt.Sprintf("dns:%d", op.ResourceID) {
			return jobs.ErrOutcomeUnverified
		}
		status = op.Status
	}
	if status != string(in.Outcome) {
		return jobs.ErrOutcomeUnverified
	}
	return nil
}
