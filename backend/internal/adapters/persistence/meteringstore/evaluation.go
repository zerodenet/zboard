package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// EvaluationWriter is an internal execution boundary. Authorization of an
// administrative trigger belongs to the service initiating the evaluation.
type EvaluationWriter struct {
	DB    *gorm.DB
	Actor uint
}

func (s EvaluationWriter) Apply(ctx context.Context, subscription uint, policy metering.Policy, observation metering.EvaluationObservation, now time.Time) (metering.EvaluationChange, error) {
	var out metering.EvaluationChange
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if s.Actor != 0 {
			var admin model.User
			err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND is_admin = ? AND status = ?", s.Actor, true, "active").Select("id").First(&admin).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return metering.ErrPolicyPermission
			}
			if err != nil {
				return err
			}
		}
		// Serialize first-state creation as well as later updates across workers.
		var sub model.Subscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "plan_id").First(&sub, subscription).Error; err != nil {
			return err
		}
		// Current locking reads protect the selected override and absent override
		// ranges until state/event commit. A repeatable-read snapshot is insufficient.
		resolution, err := resolvePolicy(tx.Clauses(clause.Locking{Strength: "SHARE"}), metering.PolicyScope{Type: "subscription", ID: subscription}, sub.PlanID)
		if err != nil {
			return err
		}
		if !metering.SameEvaluationPolicy(policy, resolution.Effective) {
			return metering.ErrEvaluationPolicyChanged
		}
		if !policy.Enabled {
			out = metering.EvaluationChange{Skipped: true, Reason: "policy_disabled"}
			return nil
		}
		var state StateRecord
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("subscription_id = ?", subscription).First(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state = StateRecord{SubscriptionID: subscription, State: "normal", TelemetryCompleteness: "unknown", CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&state).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		out = metering.TransitionEvaluation(policy, metering.State(state), observation, now)
		if out.Reason == "evaluation_interval_not_due" {
			return nil
		}
		updated := StateRecord(out.State)
		if err := tx.Model(&StateRecord{}).Where("subscription_id = ?", subscription).Select("score", "state", "current_active_flows", "connection_starts", "working_nodes", "telemetry_completeness", "last_evaluated_at", "last_complete_at", "updated_at").Updates(&updated).Error; err != nil {
			return err
		}
		if event := out.Event; event != nil {
			row := EventRecord{SubscriptionID: subscription, EventType: event.EventType, ScoreBefore: event.ScoreBefore, ScoreAfter: event.ScoreAfter, StateBefore: event.StateBefore, StateAfter: event.StateAfter, MetricsJSON: string(event.Metrics), Reason: event.Reason, OccurredAt: event.OccurredAt, CreatedAt: now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			event.ID = row.ID
		}
		return nil
	})
	if err != nil {
		return metering.EvaluationChange{}, err
	}
	return out, nil
}
