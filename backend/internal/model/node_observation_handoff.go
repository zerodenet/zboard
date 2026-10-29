package model

import "time"

// These tables intentionally have no nodes/financial FK. Deleting a node must
// retain pending sources, held evidence and its exact-asset tombstone.
type NodeObservationIntent struct {
	// Original material size/count and identity stay immutable after retirement.
	MaterialRetiredAt *time.Time `gorm:"-:migration"`
	MaterialReviewAt  time.Time  `gorm:"not null;default:('1970-01-01 00:00:00.000');-:migration"`
	ID                string     `gorm:"primaryKey;size:64"`
	NodeID            uint       `gorm:"not null"`
	AssetTag          string     `gorm:"size:64;not null"`
	Status            string     `gorm:"size:24;not null;index:idx_node_observation_ready,priority:1"`
	AvailableAt       time.Time  `gorm:"not null;index:idx_node_observation_ready,priority:2"`
	Token             string     `gorm:"size:64;not null;default:''"`
	LeaseUntil        time.Time  `gorm:"not null"`
	MaterialBytes     uint64     `gorm:"not null"`
	PartCount         uint64     `gorm:"not null"`
	Attempts          uint64     `gorm:"not null;default:0"`
	Reason            string     `gorm:"size:64;not null;default:''"`
	CreatedAt         time.Time  `gorm:"not null"`
	UpdatedAt         time.Time  `gorm:"not null"`
}

func (NodeObservationIntent) TableName() string { return "node_observation_intents" }

type NodeObservationPart struct {
	IntentID string `gorm:"primaryKey;size:64"`
	Ordinal  uint64 `gorm:"primaryKey;autoIncrement:false"`
	Data     []byte `gorm:"type:blob;not null"`
}

func (NodeObservationPart) TableName() string { return "node_observation_parts" }

type NodeObservationState struct {
	NodeID          uint   `gorm:"primaryKey;autoIncrement:false"`
	AssetTag        string `gorm:"primaryKey;size:64"`
	LatestKnown     bool   `gorm:"not null;default:false"`
	StatsKnown      bool   `gorm:"not null;default:false"`
	LatestReference string `gorm:"size:64;not null;default:''"`
	StatsReference  string `gorm:"size:64;not null;default:''"`
	// Empty asset state holds an unattributed pre-cutover floor only.
	FloorReference string `gorm:"size:64;not null;default:''"`
	Deleted        bool   `gorm:"not null;default:false"`
	CurrentKnown   bool   `gorm:"not null;default:false"`
	CurrentSource  string `gorm:"size:32;not null;default:'unknown'"`
	// Despite the historical internal name, this is the current counter
	// source's RECEIVER time, not apply/commit time. Keep ns fence separately.
	LatestOccurredAt    *time.Time
	LatestReceivedAt    *time.Time
	StatsOccurredAt     *time.Time
	StatsReceivedAt     *time.Time
	CounterWrittenStamp string `gorm:"size:64;not null;default:''"`
	CounterWrittenAt    *time.Time
	UpdatedAt           time.Time `gorm:"not null"`
}

func (NodeObservationState) TableName() string { return "node_observation_states" }
