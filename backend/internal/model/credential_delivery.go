package model

import "time"

// Derived exclusively from a deployment's frozen manifest and exact config
// confirmation. These rows do not grant business access or describe liveness.
type CredentialDeliveryConfirmation struct {
	EndpointSHA256     string    `gorm:"size:64;not null"`
	CredentialID       uint      `gorm:"primaryKey;autoIncrement:false"`
	NodeID             uint      `gorm:"not null;index"`
	ProtocolEndpointID uint      `gorm:"not null"`
	DeploymentID       uint      `gorm:"not null;index"`
	MaterialSHA256     string    `gorm:"size:64;not null"`
	ConfigSHA256       string    `gorm:"size:64;not null"`
	PolicyRevision     uint64    `gorm:"not null"`
	ConfirmedAt        time.Time `gorm:"not null"`
}

type NodeCredentialDeliveryState struct {
	NodeID       uint      `gorm:"primaryKey;autoIncrement:false"`
	DeploymentID uint      `gorm:"not null"`
	ConfigSHA256 string    `gorm:"size:64;not null"`
	ConfirmedAt  time.Time `gorm:"not null"`
}

type NetworkEntryDeliveryConfirmation struct {
	EntryID        uint      `gorm:"primaryKey;autoIncrement:false"`
	NodeID         uint      `gorm:"not null;index"`
	DeploymentID   uint      `gorm:"not null"`
	MaterialSHA256 string    `gorm:"size:64;not null"`
	ConfigSHA256   string    `gorm:"size:64;not null"`
	ConfirmedAt    time.Time `gorm:"not null"`
}
