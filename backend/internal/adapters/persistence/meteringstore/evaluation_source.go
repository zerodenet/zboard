package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

// EvaluationSource reads internal scheduler input. It is not an actor-authorized public API.
type EvaluationSource struct{ DB *gorm.DB }

func (s EvaluationSource) Snapshot(ctx context.Context, id uint) (metering.EvaluationSnapshot, error) {
	var out metering.EvaluationSnapshot
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sub model.Subscription
		if err := tx.Select("id", "plan_id").First(&sub, id).Error; err != nil {
			return err
		}
		resolution, err := resolvePolicy(tx, metering.PolicyScope{Type: "subscription", ID: id}, sub.PlanID)
		if err != nil {
			return err
		}
		out.Policy = resolution.Effective
		out.Source = resolution.Source
		var state StateRecord
		err = tx.Where("subscription_id = ?", id).First(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		out.State = metering.State(state)
		return nil
	})
	if err != nil {
		return metering.EvaluationSnapshot{}, err
	}
	return out, nil
}
func (s EvaluationSource) Candidates(ctx context.Context, now time.Time) ([]uint, error) {
	var ids []uint
	dueExpression := `(evaluation_state.last_evaluated_at IS NULL OR TIMESTAMPADD(SECOND,
			COALESCE(subscription_policy.evaluation_interval_seconds, plan_policy.evaluation_interval_seconds,
				platform_policy.evaluation_interval_seconds, ?), evaluation_state.last_evaluated_at) <= ?)`
	if s.DB.WithContext(ctx).Dialector.Name() == "sqlite" {
		dueExpression = `(evaluation_state.last_evaluated_at IS NULL OR
			julianday(evaluation_state.last_evaluated_at) + (COALESCE(
				subscription_policy.evaluation_interval_seconds, plan_policy.evaluation_interval_seconds,
				platform_policy.evaluation_interval_seconds, ?) / 86400.0) <= julianday(?))`
	}
	err := s.DB.WithContext(ctx).Table("subscriptions").
		Select("subscriptions.id").
		Joins("LEFT JOIN fair_use_policies AS subscription_policy ON subscription_policy.scope_type = ? AND subscription_policy.scope_id = subscriptions.id", "subscription").
		Joins("LEFT JOIN fair_use_policies AS plan_policy ON plan_policy.scope_type = ? AND plan_policy.scope_id = subscriptions.plan_id", "plan").
		Joins("LEFT JOIN fair_use_policies AS platform_policy ON platform_policy.scope_type = ? AND platform_policy.scope_id = ?", "platform", 0).
		Joins("LEFT JOIN subscription_fair_use_states AS evaluation_state ON evaluation_state.subscription_id = subscriptions.id").
		Where("subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", "active", now).
		Where("COALESCE(subscription_policy.enabled, plan_policy.enabled, platform_policy.enabled, ?) = ?", false, true).
		Where(dueExpression, 60, now).
		Order("evaluation_state.last_evaluated_at IS NOT NULL asc, evaluation_state.last_evaluated_at asc, subscriptions.id asc").
		Limit(100).
		Pluck("subscriptions.id", &ids).Error
	return ids, err
}
