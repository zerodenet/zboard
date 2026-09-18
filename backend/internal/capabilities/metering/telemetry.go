package metering

import (
	"context"
	"time"
)

type WindowMetric struct {
	WindowSeconds int   `json:"window_seconds"`
	Count         int64 `json:"count"`
}

type TelemetryMetrics struct {
	SubscriptionID           uint            `json:"subscription_id"`
	UserID                   uint            `json:"user_id"`
	SampledAt                time.Time       `json:"sampled_at"`
	CurrentActiveFlows       *uint64         `json:"current_active_flows"`
	ConnectionStarts         WindowMetric    `json:"connection_starts"`
	ReceivedConnectionStarts WindowMetric    `json:"received_connection_starts"`
	WorkingNodes             WindowMetric    `json:"working_nodes"`
	ReceivedWorkingNodes     WindowMetric    `json:"received_working_nodes"`
	LastActivityAt           *time.Time      `json:"last_activity_at"`
	LastReceivedAt           *time.Time      `json:"last_received_at"`
	TelemetryCompleteness    string          `json:"telemetry_completeness"`
	EvaluationReady          bool            `json:"evaluation_ready"`
	EnforcementReady         bool            `json:"enforcement_ready"`
	EventTimeBasis           string          `json:"event_time_basis"`
	Coverage                 CoverageSummary `json:"coverage"`
}

type TelemetryRepository interface {
	Read(context.Context, uint, uint, int, int, time.Time) (TelemetryMetrics, error)
}
type Telemetry struct{ Repository TelemetryRepository }

func (s Telemetry) Read(ctx context.Context, actor, subscription uint, connectionWindow, nodeWindow int, now time.Time) (TelemetryMetrics, error) {
	if err := validateScope(actor, PolicyScope{Type: "subscription", ID: subscription}); err != nil {
		return TelemetryMetrics{}, err
	}
	if connectionWindow < 10 || connectionWindow > 3600 || nodeWindow < 30 || nodeWindow > 3600 {
		return TelemetryMetrics{}, &PolicyValidation{Fields: map[string]string{"window": "outside supported telemetry range"}}
	}
	return s.Repository.Read(ctx, actor, subscription, connectionWindow, nodeWindow, now)
}
