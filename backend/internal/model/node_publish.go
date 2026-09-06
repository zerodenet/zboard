package model

import "time"

// NodeConfigPublish retains only the latest desired publication per node.
// LeaseToken fences acknowledgements from expired workers.
type NodeConfigPublish struct {
	NodeID        uint      `gorm:"primaryKey;autoIncrement:false"`
	EndpointID    uint      `gorm:"not null"`
	RequestedBy   uint      `gorm:"not null"`
	Generation    uint64    `gorm:"not null;default:1"`
	Attempts      uint      `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null;index:idx_node_publish_due,priority:1"`
	LeaseUntil    time.Time `gorm:"not null;index:idx_node_publish_due,priority:2"`
	LeaseToken    string    `gorm:"size:36;not null"`
	LastError     string    `gorm:"size:1000;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Node          *Node `gorm:"foreignKey:NodeID;constraint:OnDelete:CASCADE"`
}
