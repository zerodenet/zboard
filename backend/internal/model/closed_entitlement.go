package model

import "time"

// ClosedEntitlement preserves the closed quota budget for late authenticated
// flows. Only its metered FlowUsed advances after closing; the current
// subscription and commercial settlement snapshots remain independent.
type ClosedEntitlement struct {
	SubscriptionID    uint      `gorm:"primaryKey;autoIncrement:false"`
	Epoch             uint64    `gorm:"primaryKey;autoIncrement:false"`
	UserID            uint      `gorm:"not null"`
	TransitionOrderID uint      `gorm:"not null"`
	FlowTotal         int64     `gorm:"not null"`
	ClosingFlowUsed   int64     `gorm:"not null"`
	FlowUsed          int64     `gorm:"not null"`
	TrafficCalcMode   int16     `gorm:"not null"`
	StartAt           time.Time `gorm:"not null"`
	EndAt             time.Time `gorm:"not null"`
	ClosedAt          time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}
