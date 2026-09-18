package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"time"
)

type Telemetry struct{ DB *gorm.DB }

func (s Telemetry) Read(ctx context.Context, actor, subscription uint, connectionWindow, nodeWindow int, now time.Time) (metering.TelemetryMetrics, error) {
	var out metering.TelemetryMetrics
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := observationReader(tx, actor, subscription); err != nil {
			return err
		}
		var err error
		out, err = loadTelemetry(tx, subscription, connectionWindow, nodeWindow, now)
		return err
	})
	if err != nil {
		return metering.TelemetryMetrics{}, err
	}
	return out, nil
}
func (s Telemetry) Sample(ctx context.Context, subscription uint, connectionWindow, nodeWindow int, now time.Time) (metering.TelemetryMetrics, error) {
	var out metering.TelemetryMetrics
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = loadTelemetry(tx, subscription, connectionWindow, nodeWindow, now)
		return err
	})
	if err != nil {
		return metering.TelemetryMetrics{}, err
	}
	return out, nil
}
func loadTelemetry(db *gorm.DB, subscriptionID uint, connectionWindow, workingNodeWindow int, now time.Time) (metering.TelemetryMetrics, error) {
	var subscription model.Subscription
	if err := db.Select("id", "user_id").First(&subscription, subscriptionID).Error; err != nil {
		return metering.TelemetryMetrics{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	metrics := metering.TelemetryMetrics{
		SubscriptionID: subscription.ID,
		UserID:         subscription.UserID,
		SampledAt:      now,
		ConnectionStarts: metering.WindowMetric{
			WindowSeconds: connectionWindow,
		},
		ReceivedConnectionStarts: metering.WindowMetric{
			WindowSeconds: connectionWindow,
		},
		WorkingNodes: metering.WindowMetric{
			WindowSeconds: workingNodeWindow,
		},
		ReceivedWorkingNodes: metering.WindowMetric{
			WindowSeconds: workingNodeWindow,
		},
		TelemetryCompleteness: "unknown",
		EvaluationReady:       false,
		EnforcementReady:      false,
		EventTimeBasis:        "core_event_time_diagnostic_receive_time_evaluation",
	}

	connectionCutoff := now.Add(-time.Duration(connectionWindow) * time.Second)
	if err := db.Model(&FlowStartRecord{}).
		Where("subscription_id = ? AND occurred_at >= ? AND occurred_at <= ?", subscription.ID, connectionCutoff, now).
		Count(&metrics.ConnectionStarts.Count).Error; err != nil {
		return metrics, err
	}
	if err := db.Model(&FlowStartRecord{}).
		Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscription.ID, connectionCutoff, now).
		Count(&metrics.ReceivedConnectionStarts.Count).Error; err != nil {
		return metrics, err
	}

	workingNodeCutoff := now.Add(-time.Duration(workingNodeWindow) * time.Second)
	if err := db.Model(&FlowStartRecord{}).
		Where("subscription_id = ? AND occurred_at >= ? AND occurred_at <= ?", subscription.ID, workingNodeCutoff, now).
		Distinct("node_id").Count(&metrics.WorkingNodes.Count).Error; err != nil {
		return metrics, err
	}
	if err := db.Model(&FlowStartRecord{}).
		Where("subscription_id = ? AND received_at >= ? AND received_at <= ?", subscription.ID, workingNodeCutoff, now).
		Distinct("node_id").Count(&metrics.ReceivedWorkingNodes.Count).Error; err != nil {
		return metrics, err
	}

	var current PrincipalFlowScopeCurrent
	currentErr := db.Where("scope_type = ? AND scope_id = ?", ScopeSubscription, subscription.ID).First(&current).Error
	switch {
	case currentErr == nil:
		value := current.ActiveFlows
		metrics.CurrentActiveFlows = &value
	case !errors.Is(currentErr, gorm.ErrRecordNotFound):
		return metrics, currentErr
	}

	var latest FlowStartRecord
	latestErr := db.Where("subscription_id = ?", subscription.ID).Order("occurred_at desc, id desc").First(&latest).Error
	switch {
	case latestErr == nil:
		occurred := latest.OccurredAt.UTC()
		received := latest.ReceivedAt.UTC()
		metrics.LastActivityAt = &occurred
		metrics.LastReceivedAt = &received
	case !errors.Is(latestErr, gorm.ErrRecordNotFound):
		return metrics, latestErr
	}

	coverageWindow := connectionWindow
	if workingNodeWindow > coverageWindow {
		coverageWindow = workingNodeWindow
	}
	coverage, err := loadCoverage(db, subscription.ID, coverageWindow, now)
	if err != nil {
		return metrics, err
	}
	metrics.Coverage = coverage
	metrics.TelemetryCompleteness = coverage.State
	metrics.EvaluationReady = coverage.State == "complete"
	return metrics, nil
}
