package model

import "time"

// NodeRetirement survives removal of the node; it contains no credentials.
type NodeRetirement struct {
	NodeID    uint   `gorm:"primaryKey;autoIncrement:false"`
	NodeName  string `gorm:"size:100;not null"`
	ActorID   uint   `gorm:"not null"`
	Mode      string `gorm:"size:16;not null"`
	State     string `gorm:"size:24;not null"`
	RunID     string `gorm:"size:36;not null"`
	Attempt   int    `gorm:"not null"`
	Steps     string `gorm:"type:text;not null"`
	LastError string `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
