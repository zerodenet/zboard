package model

import "time"

// Original signed-API nonces survive detail retirement with their exact database
// identity rules. Source records are no longer their storage authority.
type MeteringNonceReceipt struct {
	NodeID   uint   `gorm:"primaryKey;autoIncrement:false"`
	Nonce    string `gorm:"primaryKey;size:64"`
	ReportID string `gorm:"size:191;not null"`
}

func (MeteringNonceReceipt) TableName() string { return "metering_nonce_receipts" }

type MeteringLedgerRetentionProgress struct {
	HighWaterID    uint      `gorm:"not null"`
	HeldCount      uint64    `gorm:"not null"`
	LastHeldID     uint      `gorm:"not null"`
	LastHoldReason string    `gorm:"size:255;not null"`
	ID             uint      `gorm:"primaryKey;autoIncrement:false"`
	AfterID        uint      `gorm:"not null"`
	UpperID        uint      `gorm:"not null"`
	NextPassAt     time.Time `gorm:"not null"`
}

func (MeteringLedgerRetentionProgress) TableName() string {
	return "metering_ledger_retention_progress"
}

// One redirect per retired ledger bucket, never one rewrite per report receipt.
// A result terminates the chain after minute -> hour -> day -> lifetime.
type MeteringLedgerTarget struct {
	SourceID           uint `gorm:"primaryKey;autoIncrement:false"`
	NodeID             uint `gorm:"not null"`
	ProtocolEndpointID uint `gorm:"not null"`
	RecordID           uint `gorm:"not null"`
	ResultID           uint `gorm:"not null"`
}

func (MeteringLedgerTarget) TableName() string { return "metering_ledger_targets" }
