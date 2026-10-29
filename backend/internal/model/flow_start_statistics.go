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

// A thin exact-time point is needed only for partial minute boundaries. It and
// both minute contributions retire atomically with the original15-day raw row.
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
