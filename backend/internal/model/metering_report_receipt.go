package model

import "time"

// MeteringReportReceipt is durable accounting identity, independent of optional
// diagnostics and historical chart detail. It is committed with its source
// ledger/quota transaction. No foreign key to the ledger is intentional: a
// future verified archive must retain replay identity after moving a fact.
// This model does not itself authorize archival or deletion of either table.
type MeteringReportReceipt struct {
	NodeID             uint      `gorm:"primaryKey;autoIncrement:false;not null"`
	ReportID           string    `gorm:"primaryKey;size:191;not null"`
	ProtocolEndpointID uint      `gorm:"not null"`
	TrafficRecordID    uint      `gorm:"not null"`
	CreatedAt          time.Time `gorm:"not null"`
}

func (MeteringReportReceipt) TableName() string { return "metering_report_receipts" }
