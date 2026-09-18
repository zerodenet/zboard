package meteringstore

import "time"

type StateRecord struct {
	SubscriptionID        uint       `json:"subscription_id" gorm:"column:subscription_id;primaryKey"`
	Score                 int        `json:"score" gorm:"column:score"`
	State                 string     `json:"state" gorm:"column:state"`
	CurrentActiveFlows    *uint64    `json:"current_active_flows" gorm:"column:current_active_flows"`
	ConnectionStarts      int        `json:"connection_starts" gorm:"column:connection_starts"`
	WorkingNodes          int        `json:"working_nodes" gorm:"column:working_nodes"`
	TelemetryCompleteness string     `json:"telemetry_completeness" gorm:"column:telemetry_completeness"`
	LastEvaluatedAt       *time.Time `json:"last_evaluated_at,omitempty" gorm:"column:last_evaluated_at"`
	LastCompleteAt        *time.Time `json:"last_complete_at,omitempty" gorm:"column:last_complete_at"`
	CreatedAt             time.Time  `json:"created_at" gorm:"column:created_at"`
	UpdatedAt             time.Time  `json:"updated_at" gorm:"column:updated_at"`
}

func (StateRecord) TableName() string { return "subscription_fair_use_states" }

type EventRecord struct {
	ID             uint64    `json:"id" gorm:"column:id;primaryKey"`
	SubscriptionID uint      `json:"subscription_id" gorm:"column:subscription_id"`
	EventType      string    `json:"event_type" gorm:"column:event_type"`
	ScoreBefore    int       `json:"score_before" gorm:"column:score_before"`
	ScoreAfter     int       `json:"score_after" gorm:"column:score_after"`
	StateBefore    string    `json:"state_before" gorm:"column:state_before"`
	StateAfter     string    `json:"state_after" gorm:"column:state_after"`
	MetricsJSON    string    `json:"-" gorm:"column:metrics_json"`
	Reason         string    `json:"reason" gorm:"column:reason"`
	OccurredAt     time.Time `json:"occurred_at" gorm:"column:occurred_at"`
	CreatedAt      time.Time `json:"created_at" gorm:"column:created_at"`
}

func (EventRecord) TableName() string { return "subscription_fair_use_events" }
