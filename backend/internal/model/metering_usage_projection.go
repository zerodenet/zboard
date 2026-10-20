package model

import "time"

// MeteringUsageProjectionPending is a durable per-ledger projection checkpoint.
// It is inserted with settlement and removed only with the derived daily delta.
// A global maximum TrafficRecord ID is unsafe: transactions can commit IDs out
// of order. Pending rows also keep their source facts live until projection.
type MeteringUsageProjectionPending struct {
	TrafficRecordID    uint      `gorm:"primaryKey;autoIncrement:false;index:idx_usage_projection_endpoint,priority:2"`
	ProtocolEndpointID uint      `gorm:"not null;index:idx_usage_projection_endpoint,priority:1"`
	CreatedAt          time.Time `gorm:"not null"`
}

func (MeteringUsageProjectionPending) TableName() string {
	return "metering_usage_projection_pending"
}
