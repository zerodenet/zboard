package meteringstore

import "time"

type PolicyRecord struct {
	ScopeType                    string    `json:"scope_type" gorm:"column:scope_type;primaryKey"`
	ScopeID                      uint      `json:"scope_id" gorm:"column:scope_id;primaryKey"`
	Enabled                      bool      `json:"enabled" gorm:"column:enabled"`
	EvaluationIntervalSeconds    int       `json:"evaluation_interval_seconds" gorm:"column:evaluation_interval_seconds"`
	ConnectionStartThreshold     int       `json:"connection_start_threshold" gorm:"column:connection_start_threshold"`
	ConnectionStartWindowSeconds int       `json:"connection_start_window_seconds" gorm:"column:connection_start_window_seconds"`
	ConnectionStartPenalty       int       `json:"connection_start_penalty" gorm:"column:connection_start_penalty"`
	WorkingNodeThreshold         int       `json:"working_node_threshold" gorm:"column:working_node_threshold"`
	WorkingNodeWindowSeconds     int       `json:"working_node_window_seconds" gorm:"column:working_node_window_seconds"`
	WorkingNodePenalty           int       `json:"working_node_penalty" gorm:"column:working_node_penalty"`
	ScoreMax                     int       `json:"score_max" gorm:"column:score_max"`
	RecoveryPerInterval          int       `json:"recovery_per_interval" gorm:"column:recovery_per_interval"`
	WarningScore                 int       `json:"warning_score" gorm:"column:warning_score"`
	ViolationScore               int       `json:"violation_score" gorm:"column:violation_score"`
	EnforcementMode              string    `json:"enforcement_mode" gorm:"column:enforcement_mode"`
	RestrictionDurationSeconds   int       `json:"restriction_duration_seconds" gorm:"column:restriction_duration_seconds"`
	Revision                     uint64    `json:"revision" gorm:"column:revision"`
	CreatedAt                    time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt                    time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (PolicyRecord) TableName() string { return "fair_use_policies" }
