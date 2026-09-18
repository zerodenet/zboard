package meteringstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Observations struct{ DB *gorm.DB }

func observationReader(tx *gorm.DB, actor, subscription uint) error {
	var user model.User
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ? AND is_admin = ? AND status = ?", actor, true, "active").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return metering.ErrPolicyPermission
	}
	if err != nil {
		return err
	}
	var sub model.Subscription
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").First(&sub, subscription).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return metering.ErrPolicyNotFound
	}
	return err
}
func (s Observations) State(ctx context.Context, actor, subscription uint) (metering.State, error) {
	var out metering.State
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := observationReader(tx, actor, subscription); err != nil {
			return err
		}
		var row StateRecord
		err := tx.Where("subscription_id = ?", subscription).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			out = metering.State{SubscriptionID: subscription, State: "normal", TelemetryCompleteness: "unknown"}
			return nil
		}
		if err != nil {
			return err
		}
		out = metering.State(row)
		return nil
	})
	if err != nil {
		return metering.State{}, err
	}
	return out, nil
}
func (s Observations) Events(ctx context.Context, actor, subscription uint, limit int) ([]metering.Event, error) {
	out := make([]metering.Event, 0)
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := observationReader(tx, actor, subscription); err != nil {
			return err
		}
		var rows []EventRecord
		if err := tx.Where("subscription_id = ?", subscription).Order("occurred_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			metrics := json.RawMessage(row.MetricsJSON)
			if !json.Valid(metrics) {
				metrics = json.RawMessage(`{}`)
			}
			out = append(out, metering.Event{ID: row.ID, SubscriptionID: row.SubscriptionID, EventType: row.EventType, ScoreBefore: row.ScoreBefore, ScoreAfter: row.ScoreAfter, StateBefore: row.StateBefore, StateAfter: row.StateAfter, Metrics: metrics, Reason: row.Reason, OccurredAt: row.OccurredAt.UTC()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
