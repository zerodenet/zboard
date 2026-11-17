package model

import "time"

// One mailbox per live node, never per event or runtime generation. The FK is
// migration-owned: deleting the asset also removes its disposable projection.
type NodeObservationMailbox struct {
	NodeID       uint      `gorm:"primaryKey;autoIncrement:false;index:idx_observation_mailbox_pending,priority:3"`
	AssetTag     string    `gorm:"size:64;not null"`
	Observation  string    `gorm:"type:text;not null"`
	Pending      bool      `gorm:"not null;index:idx_observation_mailbox_pending,priority:1"`
	PendingSince time.Time `gorm:"not null;default:('1970-01-01 00:00:00.000');index:idx_observation_mailbox_pending,priority:2"`
	Reason       string    `gorm:"size:64;not null;default:''"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (NodeObservationMailbox) TableName() string { return "node_observation_mailboxes" }
