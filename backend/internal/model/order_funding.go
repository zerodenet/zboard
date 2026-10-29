package model

import "time"

// One immutable funding split per order; only its lifecycle may advance.
type OrderFunding struct {
	// EntitlementMinor is not reserved cash until the old value is settled.
	EntitlementMinor int64     `gorm:"not null;default:0;-:migration"`
	OrderID          uint      `gorm:"primaryKey;autoIncrement:false"`
	UserID           uint      `gorm:"not null;index"`
	Currency         string    `gorm:"size:16;not null"`
	UseBalance       bool      `gorm:"not null"`
	AmountMinor      int64     `gorm:"not null;check:ck_order_funding_amounts,amount_minor >= 0 AND wallet_minor >= 0 AND cash_minor >= 0 AND wallet_minor + cash_minor = amount_minor"`
	WalletMinor      int64     `gorm:"not null"`
	CashMinor        int64     `gorm:"not null"`
	State            string    `gorm:"size:24;not null;index:idx_funding_expiry,priority:1"`
	ExpiresAt        time.Time `gorm:"not null;index:idx_funding_expiry,priority:2"`
	CapturedAt       *time.Time
	ReleasedAt       *time.Time
	ReleaseReason    string `gorm:"size:64;not null;default:''"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Order            *Order `gorm:"foreignKey:OrderID;constraint:OnDelete:RESTRICT"`
	User             *User  `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
}
