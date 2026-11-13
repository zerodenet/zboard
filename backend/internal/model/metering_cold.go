package model

import "time"

// Cold blocks replace many individual indexed identities with lossless bounded
// pages. Directory hashes route a lookup; complete collation keys prove it.
type MeteringColdBlock struct {
	Bloom        []byte `gorm:"type:varbinary(1024);not null"`
	HeaderSHA256 string `gorm:"size:64;not null"`
	ID           uint   `gorm:"primaryKey"`
	NodeID       uint   `gorm:"not null;uniqueIndex:uk_metering_cold_page,priority:1"`
	Kind         uint8  `gorm:"not null;uniqueIndex:uk_metering_cold_page,priority:2"`
	Depth        uint8  `gorm:"not null;uniqueIndex:uk_metering_cold_page,priority:3"`
	Prefix       uint32 `gorm:"not null;uniqueIndex:uk_metering_cold_page,priority:4"`
	Version      uint8  `gorm:"not null"`
	EntryCount   uint32 `gorm:"not null"`
	RawBytes     uint32 `gorm:"not null"`
	SHA256       string `gorm:"size:64;not null"`
	Payload      []byte `gorm:"type:mediumblob;not null"`
}

func (MeteringColdBlock) TableName() string { return "metering_cold_blocks" }

type MeteringIdentityProfile struct {
	Kind      uint8  `gorm:"primaryKey;autoIncrement:false;not null"`
	Collation string `gorm:"size:64;not null"`
	PadSpace  bool   `gorm:"not null"`
}

func (MeteringIdentityProfile) TableName() string { return "metering_identity_profiles" }

type MeteringColdProgress struct {
	HeldCount      uint64    `gorm:"not null"`
	LastHeldNodeID uint      `gorm:"not null"`
	LastHoldReason string    `gorm:"size:255;not null"`
	Kind           uint8     `gorm:"primaryKey;autoIncrement:false;not null"`
	NodeID         uint      `gorm:"not null"`
	AfterKey       string    `gorm:"size:191;not null"`
	AfterID        uint      `gorm:"not null"`
	NextPassAt     time.Time `gorm:"not null"`
}

func (MeteringColdProgress) TableName() string { return "metering_cold_progress" }

type MeteringColdRoot struct {
	NodeID     uint  `gorm:"primaryKey;autoIncrement:false;not null"`
	Kind       uint8 `gorm:"primaryKey;autoIncrement:false;not null"`
	PageCount  int64 `gorm:"not null"`
	EntryCount int64 `gorm:"not null"`
}

func (MeteringColdRoot) TableName() string { return "metering_cold_roots" }
