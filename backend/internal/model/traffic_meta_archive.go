package model

import "time"

// TrafficMetaArchive owns a lossless copy of native accounting proof material.
// The ordinary ledger row retains every amount, scope, nonce and report identity.
// Payloads are immutable; an absent recorded archive is never rebuilt as empty.
type TrafficMetaArchive struct {
	TrafficRecordID uint      `gorm:"primaryKey;autoIncrement:false"`
	Version         uint64    `gorm:"not null"`
	SourceBytes     uint64    `gorm:"not null"`
	SourceSHA256    string    `gorm:"size:64;not null"`
	FactSHA256      string    `gorm:"size:64;not null"`
	CompressedBytes uint64    `gorm:"not null"`
	Payload         []byte    `gorm:"type:longblob;not null"`
	CreatedAt       time.Time `gorm:"not null"`
}

func (TrafficMetaArchive) TableName() string { return "traffic_record_meta_archives" }
