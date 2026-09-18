package networkstore

import "time"

type ObservationCursor struct {
	NodeID         uint      `gorm:"column:node_id;primaryKey"`
	CoreInstanceID string    `gorm:"column:core_instance_id"`
	Sequence       uint64    `gorm:"column:sequence"`
	ConfigRevision uint64    `gorm:"column:config_revision"`
	OccurredAt     time.Time `gorm:"column:occurred_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (ObservationCursor) TableName() string { return "zero_event_node_cursors" }
