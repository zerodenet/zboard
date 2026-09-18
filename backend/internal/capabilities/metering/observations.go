package metering

import (
	"context"
	"encoding/json"
	"time"
)

type State struct {
	SubscriptionID        uint       `json:"subscription_id"`
	Score                 int        `json:"score"`
	State                 string     `json:"state"`
	CurrentActiveFlows    *uint64    `json:"current_active_flows"`
	ConnectionStarts      int        `json:"connection_starts"`
	WorkingNodes          int        `json:"working_nodes"`
	TelemetryCompleteness string     `json:"telemetry_completeness"`
	LastEvaluatedAt       *time.Time `json:"last_evaluated_at,omitempty"`
	LastCompleteAt        *time.Time `json:"last_complete_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type Event struct {
	ID             uint64          `json:"id"`
	SubscriptionID uint            `json:"subscription_id"`
	EventType      string          `json:"event_type"`
	ScoreBefore    int             `json:"score_before"`
	ScoreAfter     int             `json:"score_after"`
	StateBefore    string          `json:"state_before"`
	StateAfter     string          `json:"state_after"`
	Metrics        json.RawMessage `json:"metrics"`
	Reason         string          `json:"reason"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

type ObservationRepository interface {
	State(context.Context, uint, uint) (State, error)
	Events(context.Context, uint, uint, int) ([]Event, error)
}
type Observations struct{ Repository ObservationRepository }

func (s Observations) State(ctx context.Context, actor, subscription uint) (State, error) {
	if err := validateScope(actor, PolicyScope{Type: "subscription", ID: subscription}); err != nil {
		return State{}, err
	}
	return s.Repository.State(ctx, actor, subscription)
}
func (s Observations) Events(ctx context.Context, actor, subscription uint, limit int) ([]Event, error) {
	if err := validateScope(actor, PolicyScope{Type: "subscription", ID: subscription}); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, &PolicyValidation{Fields: map[string]string{"limit": "must be between 1 and 200"}}
	}
	return s.Repository.Events(ctx, actor, subscription, limit)
}
