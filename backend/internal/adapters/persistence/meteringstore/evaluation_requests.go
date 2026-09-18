package meteringstore

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type EvaluationRequests struct{ DB *gorm.DB }

func (s EvaluationRequests) Authorize(ctx context.Context, actor, subscription uint) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return observationReader(tx, actor, subscription) })
}
func (s EvaluationRequests) Enqueue(ctx context.Context, actor, subscription uint) (metering.EvaluationReceipt, error) {
	var out metering.EvaluationReceipt
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := observationReader(tx, actor, subscription); err != nil {
			return err
		}
		payload, err := json.Marshal(metering.EvaluationRequest{Revision: "1", Actor: actor, SubscriptionID: subscription})
		if err != nil {
			return err
		}
		run, err := jobstore.New(tx).Submit(ctx, jobs.Submission{Owner: "system", Key: metering.EvaluationJob + ":" + uuid.NewString(), Handler: metering.EvaluationJob, Resource: "core:fair_use", Timeout: 5 * time.Minute, Payload: string(payload)})
		if err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{UserID: &actor, Action: "fair_use.evaluation.request", Target: fmt.Sprintf("subscription:%d", subscription), Detail: "run_id=" + run.ID}).Error; err != nil {
			return err
		}
		out = metering.EvaluationReceipt{RunID: run.ID, State: string(run.State), SubscriptionID: subscription}
		return nil
	})
	if err != nil {
		return metering.EvaluationReceipt{}, err
	}
	return out, nil
}
