package model

import "time"

// MeteringUsageProjectionPending is a durable per-ledger projection checkpoint.
// It is inserted with settlement and removed only with the derived deltas.
// A global maximum TrafficRecord ID is unsafe: transactions can commit IDs out
// of order. Pending rows also keep their source facts live until projection.
type MeteringUsageProjectionPending struct {
	TrafficRecordID    uint      `gorm:"primaryKey;autoIncrement:false;index:idx_usage_projection_endpoint,priority:2"`
	ProtocolEndpointID uint      `gorm:"not null;index:idx_usage_projection_endpoint,priority:1"`
	CreatedAt          time.Time `gorm:"not null"`
	// False on pre-0054 intents: their hourly delta was already written by
	// the synchronous accounting path. New facts explicitly set this true.
	HourlyPending      bool       `gorm:"not null;default:false"`
	UserID             uint       `gorm:"not null;default:0"`
	SubscriptionID     uint       `gorm:"not null;default:0"`
	NodeID             uint       `gorm:"not null;default:0"`
	DeltaRawBytes      int64      `gorm:"not null;default:0;-:migration"`
	DeltaUploadBytes   int64      `gorm:"not null;default:0;-:migration"`
	DeltaDownloadBytes int64      `gorm:"not null;default:0;-:migration"`
	DeltaUsedBytes     int64      `gorm:"not null;default:0;-:migration"`
	DeltaRecordCount   int64      `gorm:"not null;default:0;-:migration"`
	RecordAt           *time.Time // Actual projected UTC hour; raw time remains in TrafficRecord.
}

func (MeteringUsageProjectionPending) TableName() string {
	return "metering_usage_projection_pending"
}
