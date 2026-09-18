package meteringstore

import "time"

type CoverageRecord struct {
	NodeID              uint       `gorm:"column:node_id;primaryKey"`
	CoreInstanceID      string     `gorm:"column:core_instance_id"`
	LastSequence        uint64     `gorm:"column:last_sequence"`
	LastEventID         string     `gorm:"column:last_event_id"`
	ContinuousSinceAt   time.Time  `gorm:"column:continuous_since_at"`
	LastReceivedAt      time.Time  `gorm:"column:last_received_at"`
	LastEventOccurredAt time.Time  `gorm:"column:last_event_occurred_at"`
	LastGapFromSequence uint64     `gorm:"column:last_gap_from_sequence"`
	LastGapToSequence   uint64     `gorm:"column:last_gap_to_sequence"`
	LastGapAt           *time.Time `gorm:"column:last_gap_at"`
	GapCount            uint64     `gorm:"column:gap_count"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (CoverageRecord) TableName() string { return "fair_use_node_coverage" }
