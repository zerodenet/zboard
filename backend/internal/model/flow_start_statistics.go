package model

import "time"

// Separate from financial ledgers/cumulative cursors. Legacy source bounds are
// captured by owned quiescent preparation; new sources carry a durable intent.
type FlowStartStatisticsState struct {
	ID                                                                                             uint `gorm:"primaryKey;autoIncrement:false"`
	LegacyUpperID, LegacyAfterID, LegacyExpectedRows, LegacyProjectedRows, ProjectedHead, Revision uint64
	CreatedAt, UpdatedAt                                                                           time.Time
}

func (FlowStartStatisticsState) TableName() string { return "flow_start_statistics_state" }

type FlowStartStatisticsPending struct {
	SourceID                          uint64 `gorm:"primaryKey;autoIncrement:false"`
	SubscriptionID, NodeID            uint
	OccurredAt, ReceivedAt, CreatedAt time.Time
}

func (FlowStartStatisticsPending) TableName() string { return "flow_start_statistics_pending" }

// Exact-time points and raw diagnostics retire together after 24 hours. Minute
// history has its own 15-day expiry and survives retirement of individual points.
type FlowStartStatisticsPoint struct {
	SourceID                                          uint64 `gorm:"primaryKey;autoIncrement:false"`
	SubscriptionID, NodeID                            uint
	OccurredAt, ReceivedAt, SourceCreatedAt, ReviewAt time.Time
	FactSHA256                                        string
}

func (FlowStartStatisticsPoint) TableName() string { return "flow_start_statistics_points" }

type FlowStartStatisticsMinute struct {
	SubscriptionID uint      `gorm:"primaryKey;autoIncrement:false"`
	NodeID         uint      `gorm:"primaryKey;autoIncrement:false"`
	Basis          string    `gorm:"primaryKey"`
	MinuteAt       time.Time `gorm:"primaryKey"`
	EventCount     uint64
}

func (FlowStartStatisticsMinute) TableName() string { return "flow_start_statistics_minutes" }

// Captures a finite pre-upgrade point range for bounded deadline conversion.
// It is operational progress, never a substitute for financial replay receipts.
type FlowDetailRetentionState struct {
	ID      uint   `gorm:"primaryKey;autoIncrement:false;not null"`
	UpperID uint64 `gorm:"not null"`
	AfterID uint64 `gorm:"not null"`
}
