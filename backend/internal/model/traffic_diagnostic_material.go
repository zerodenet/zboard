package model

import "time"

// Only the legacy authenticated report writer creates these provenance rows.
// A missing row never permits classifying historical/native Meta as diagnostic.
// Financial record identities and this compact original-material receipt remain.
type TrafficDiagnosticMaterial struct {
	TrafficRecordID uint   `gorm:"primaryKey;autoIncrement:false"`
	SourceBytes     uint64 `gorm:"not null"`
	SourceSHA256    string `gorm:"size:64;not null"`
	RetiredAt       *time.Time
	ReviewAt        time.Time `gorm:"not null"`
	CreatedAt       time.Time `gorm:"not null"`
}
