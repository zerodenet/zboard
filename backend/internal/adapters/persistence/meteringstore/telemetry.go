package meteringstore

import (
	"context"
	"database/sql"
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
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
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
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
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
	count, _, err := flowStatisticsWindow(db, subscription.ID, "occurred", connectionCutoff, now)
	if err != nil {
		return metrics, err
	}
	metrics.ConnectionStarts.Count = count
	count, _, err = flowStatisticsWindow(db, subscription.ID, "received", connectionCutoff, now)
	if err != nil {
		return metrics, err
	}
	metrics.ReceivedConnectionStarts.Count = count
	nodeCutoff := now.Add(-time.Duration(workingNodeWindow) * time.Second)
	_, nodes, err := flowStatisticsWindow(db, subscription.ID, "occurred", nodeCutoff, now)
	if err != nil {
		return metrics, err
	}
	metrics.WorkingNodes.Count = nodes
	_, nodes, err = flowStatisticsWindow(db, subscription.ID, "received", nodeCutoff, now)
	if err != nil {
		return metrics, err
	}
	metrics.ReceivedWorkingNodes.Count = nodes

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
	latestErr := db.Select("id,occurred_at,received_at").Where("subscription_id = ?", subscription.ID).Order("occurred_at desc, id desc").First(&latest).Error
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
