package model

import "time"

// NodeConfigPublish retains only the latest desired publication per node.
// LeaseToken fences acknowledgements from expired workers.
type NodeConfigPublish struct {
	NodeID              uint      `gorm:"primaryKey;autoIncrement:false"`
	EndpointID          uint      `gorm:"not null"`
	RequestedBy         uint      `gorm:"not null"`
	Generation          uint64    `gorm:"not null;default:1"`
	Phase               string    `gorm:"size:20;not null;default:''"`
	AppliedConfigSHA256 string    `gorm:"size:64;not null;default:''"`
	Attempts            uint      `gorm:"not null"`
	Reconciling         bool      `gorm:"not null;default:false"`
	Suspended           bool      `gorm:"not null;default:false"`
	Deferred            bool      `gorm:"not null;default:false"`
	NextAttemptAt       time.Time `gorm:"not null;index:idx_node_publish_due,priority:1"`
	LeaseUntil          time.Time `gorm:"not null;index:idx_node_publish_due,priority:2"`
	LeaseToken          string    `gorm:"size:36;not null"`
	LastError           string    `gorm:"size:1000;not null"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Node                *Node `gorm:"foreignKey:NodeID;constraint:OnDelete:CASCADE"`
}
