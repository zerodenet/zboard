package model

import "time"

// Migration0067 owns these tables. They are auxiliary material authority only:
// never a financial receipt, source nonce, cumulative cursor, or quota ledger.
type NodeObservationMaterialBudget struct {
	ID            uint64 `gorm:"primaryKey;autoIncrement:false"`
	RetainedBytes uint64 `gorm:"not null"`
	Revision      uint64 `gorm:"not null"`
}

func (NodeObservationMaterialBudget) TableName() string { return "node_observation_material_budget" }

type NodeObservationRetirement struct {
	IntentID        string    `gorm:"primaryKey;size:64"`
	HeaderSHA256    string    `gorm:"size:64;not null"`
	State           string    `gorm:"size:24;not null"` // verifying, deleting, complete, quarantined
	Cause           string    `gorm:"size:64;not null"`
	SourceSummary   string    `gorm:"type:text;not null"`
	NextPart        uint64    `gorm:"not null"`
	VerifiedBytes   uint64    `gorm:"not null"`
	HashState       []byte    `gorm:"type:blob;not null"`
	DeletePart      uint64    `gorm:"not null"`
	RemainingBytes  uint64    `gorm:"not null"`
	ContentVerified bool      `gorm:"not null"`
	ReviewAt        time.Time `gorm:"not null"`
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (NodeObservationRetirement) TableName() string { return "node_observation_retirements" }

// Per-part verification avoids an unbounded digest blob for historical sources.
// These checksums pin only the still-retained optional parts, never billing.
type NodeObservationRetirementPart struct {
	IntentID string `gorm:"primaryKey;size:64"`
	Ordinal  uint64 `gorm:"primaryKey;autoIncrement:false"`
	SHA256   string `gorm:"size:64;not null"`
}

func (NodeObservationRetirementPart) TableName() string { return "node_observation_retirement_parts" }
