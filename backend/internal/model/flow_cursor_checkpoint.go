package model

import "time"

// FlowCursorCheckpoint is permanent replay/accounting authority, not telemetry.
// Cold connections need only their identity, terminal bit and accounting floor;
// endpoint activity indexes, principal strings and per-event metadata stay hot.
// No age limit is applied to this table on nodes that still accept reports.
type FlowCursorCheckpoint struct {
	NodeID               uint   `gorm:"primaryKey;autoIncrement:false;not null"`
	FlowID               string `gorm:"primaryKey;size:128;not null"`
	ProtocolCredentialID uint   `gorm:"not null"`
	Revision             uint64 `gorm:"not null"`
	RawBytes             int64  `gorm:"not null"`
	UploadBytes          int64  `gorm:"not null"`
	DownloadBytes        int64  `gorm:"not null"`
	UsedBytes            int64  `gorm:"not null"`
	IsCompleted          bool   `gorm:"not null"`
}

// A durable, rotating PK seek avoids a new index/rebuild of the large hot table.
// Skipped active/recent connections are revisited on the following pass.
type FlowCursorCompactionState struct {
	ID         uint      `gorm:"primaryKey;autoIncrement:false;not null"`
	AfterID    uint      `gorm:"not null"`
	NextPassAt time.Time `gorm:"not null"`
}
