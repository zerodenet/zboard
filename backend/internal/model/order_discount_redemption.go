package model

import "time"

// OrderDiscountRedemption is a durable provider intent, not coupon inventory.
// The immutable binding has one key per order. Desired finalization is advanced
// in the same financial transaction as payment or cancellation. No deletion or
// time-based expiry is permitted: terminal keys are replay/tombstone evidence.
type OrderDiscountRedemption struct {
	OrderID       uint      `gorm:"primaryKey;autoIncrement:false"`
	RedemptionKey string    `gorm:"size:64;not null;uniqueIndex"`
	BindingHash   string    `gorm:"size:64;not null"`
	BindingJSON   string    `gorm:"type:text;not null"`
	ProviderID    string    `gorm:"size:160;not null"`
	Desired       string    `gorm:"size:16;not null"`
	Observed      string    `gorm:"size:16;not null"`
	Token         string    `gorm:"size:512;not null"`
	Revision      uint64    `gorm:"not null"`
	LeaseToken    string    `gorm:"size:64;not null"`
	LeaseUntil    time.Time `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null;index"`
	LastError     string    `gorm:"size:64;not null"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
}
