package model

import "time"

// OrderValueBudget limits outgoing refundable/converted cash-backed value.
// It never treats a commercial discount as funding.
type OrderValueBudget struct {
	OrderID            uint   `gorm:"primaryKey;autoIncrement:false"`
	UserID             uint   `gorm:"not null;index"`
	Currency           string `gorm:"size:16;not null"`
	FundingMinor       int64  `gorm:"not null;check:ck_value_budget,funding_minor >= 0 AND refunded_minor >= 0 AND converted_minor >= 0 AND refunded_minor <= funding_minor AND converted_minor <= funding_minor - refunded_minor"`
	RefundedMinor      int64  `gorm:"not null;default:0"`
	ConvertedMinor     int64  `gorm:"not null;default:0"`
	FundingFingerprint string `gorm:"size:64;not null"`
	Revision           uint64 `gorm:"not null;default:1"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Order              *Order `gorm:"foreignKey:OrderID;constraint:OnDelete:RESTRICT"`
	User               *User  `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
}
type SubscriptionValueSettlement struct {
	ID                 uint   `gorm:"primaryKey;index:idx_value_settlement_owner,priority:3"`
	UserID             uint   `gorm:"not null;index:idx_value_settlement_owner,priority:1"`
	SubscriptionID     uint   `gorm:"not null;index:idx_value_settlement_owner,priority:2"`
	RootOrderID        uint   `gorm:"not null;uniqueIndex"`
	RequestKey         string `gorm:"size:64;not null;uniqueIndex"`
	RequestFingerprint string `gorm:"size:64;not null"`
	Currency           string `gorm:"size:16;not null"`
	Kind               string `gorm:"size:24;not null"`
	AmountMinor        int64  `gorm:"not null;check:ck_value_settlement_amount,amount_minor >= 0"`
	WalletMinor        int64  `gorm:"not null;check:ck_value_settlement_split,wallet_minor >= 0 AND applied_minor >= 0 AND wallet_minor <= amount_minor AND applied_minor = amount_minor - wallet_minor"`
	AppliedMinor       int64  `gorm:"not null;default:0"`
	EntryID            *uint  `gorm:"uniqueIndex"`
	ActorID            uint   `gorm:"not null"`
	Reason             string `gorm:"size:500;not null"`
	QuoteSnapshot      string `gorm:"type:text;not null"`
	CreatedAt          time.Time
	User               *User         `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
	Subscription       *Subscription `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:RESTRICT"`
	RootOrder          *Order        `gorm:"foreignKey:RootOrderID;constraint:OnDelete:RESTRICT"`
	Entry              *WalletEntry  `gorm:"foreignKey:EntryID;constraint:OnDelete:RESTRICT"`
	Actor              *User         `gorm:"foreignKey:ActorID;constraint:OnDelete:RESTRICT"`
}
type SubscriptionValueSettlementSource struct {
	SettlementID uint                         `gorm:"primaryKey;autoIncrement:false"`
	OrderID      uint                         `gorm:"primaryKey;autoIncrement:false;index"`
	AmountMinor  int64                        `gorm:"not null;check:ck_value_settlement_source_amount,amount_minor > 0"`
	Settlement   *SubscriptionValueSettlement `gorm:"foreignKey:SettlementID;constraint:OnDelete:RESTRICT"`
	Budget       *OrderValueBudget            `gorm:"belongsTo:OrderValueBudget;foreignKey:OrderID;references:OrderID;constraint:OnDelete:RESTRICT"`
}
