package model

// MeteringSettlementResult preserves already computed charges by business scope,
// independently of optional event detail. No source/report strings are retained.
type MeteringSettlementResult struct {
	ID                      uint   `gorm:"primaryKey"`
	Key                     string `gorm:"column:result_key;uniqueIndex:uk_settlement_result_key;size:64;not null"`
	UserID                  uint   `gorm:"not null"`
	SubscriptionID          uint   `gorm:"not null;index:idx_settlement_result_subscription"`
	EntitlementEpoch        uint64 `gorm:"not null"`
	NodeID                  uint   `gorm:"not null"`
	ProtocolEndpointID      uint   `gorm:"not null"`
	TrafficCalcMode         int16  `gorm:"not null"`
	ProtocolMultiplierMilli int64  `gorm:"not null"`
	RawBytes                int64  `gorm:"not null"`
	UploadBytes             int64  `gorm:"not null"`
	DownloadBytes           int64  `gorm:"not null"`
	UsedBytes               int64  `gorm:"not null"`
	RecordCount             int64  `gorm:"not null"`
}

func (MeteringSettlementResult) TableName() string { return "metering_settlement_results" }

// The captured prefix is read exactly once. New contributions to rows already
// covered, and rows above UpperID, update results in their authority transaction.
type MeteringSettlementProgress struct {
	ID      uint `gorm:"primaryKey;autoIncrement:false"`
	AfterID uint `gorm:"not null"`
	UpperID uint `gorm:"not null"`
}

func (MeteringSettlementProgress) TableName() string { return "metering_settlement_progress" }
