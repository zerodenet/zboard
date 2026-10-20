package model

import "time"

// No row means no automatic-debit consent. The agreement is never inferred
// from a paid order, an administrator assignment, or another subscription.
type SubscriptionAutomaticRenewal struct {
	SubscriptionID   uint   `gorm:"primaryKey;autoIncrement:false;not null"`
	UserID           uint   `gorm:"not null;index:idx_automatic_renewal_due,priority:3"`
	Enabled          bool   `gorm:"not null;default:false;index:idx_automatic_renewal_due,priority:1"`
	PlanSKUID        uint   `gorm:"column:plan_sku_id;not null"`
	RootOrderID      uint   `gorm:"not null"`
	AgreementDigest  string `gorm:"size:64;not null"`
	AgreementVersion int    `gorm:"not null;default:1"`
	ApprovalSnapshot string `gorm:"type:text;not null;default:('')"`
	Revision         uint64 `gorm:"not null;default:1"`
	State            string `gorm:"size:32;not null"`
	Message          string `gorm:"size:500;not null;default:''"`
	LastOrderID      *uint
	LastAttemptAt    *time.Time
	AttemptedEndAt   *time.Time
	AttemptSequence  uint64    `gorm:"not null;default:0"`
	NextAttemptAt    time.Time `gorm:"not null;index:idx_automatic_renewal_due,priority:2"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Subscription     *Subscription `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:RESTRICT"`
	User             *User         `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
	SKU              *PlanSKU      `gorm:"foreignKey:PlanSKUID;constraint:OnDelete:RESTRICT"`
	RootOrder        *Order        `gorm:"foreignKey:RootOrderID;constraint:OnDelete:RESTRICT"`
	LastOrder        *Order        `gorm:"foreignKey:LastOrderID;constraint:OnDelete:RESTRICT"`
}

// Each settlement locks this row before choosing the current first due
// subscription. Multiple executors share the same stable wallet competition
// order and commit one bounded subscription transaction at a time.
type AutomaticRenewalScan struct {
	ID       uint   `gorm:"primaryKey;autoIncrement:false"`
	Sequence uint64 `gorm:"not null;default:0"`
}
