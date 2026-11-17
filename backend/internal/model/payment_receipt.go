package model

import "time"

// An attempt is granted by the host before a channel can collect money. It is
// retained after cancellation so a real late payment can still be reconciled.
type PaymentAttempt struct {
	ID          uint   `gorm:"primaryKey"`
	OrderID     uint   `gorm:"not null;uniqueIndex:ux_payment_attempt,priority:1"`
	Provider    string `gorm:"size:32;not null;uniqueIndex:ux_payment_attempt,priority:2"`
	AmountMinor int64  `gorm:"not null"`
	Currency    string `gorm:"size:16;not null"`
	CreatedAt   time.Time
	Order       *Order `gorm:"foreignKey:OrderID;constraint:OnDelete:RESTRICT"`
}

// PaymentReceipt identifies money by provider trade identity, independently of
// callback event IDs and commercial fulfillment. No raw secrets are stored.
type PaymentReceipt struct {
	ID               uint   `gorm:"primaryKey"`
	ReceiptKey       string `gorm:"size:64;not null;uniqueIndex"`
	Provider         string `gorm:"size:32;not null"`
	ProviderTradeNo  string `gorm:"size:128;not null"`
	ResolvedBy       *uint  `gorm:"index"`
	TicketID         *uint  `gorm:"index"`
	ResolutionReason string `gorm:"size:500;not null;default:''"`
	ResolvedAt       *time.Time
	Resolver         *AccountSubject `gorm:"foreignKey:ResolvedBy;constraint:OnDelete:RESTRICT"`
	Ticket           *Ticket         `gorm:"foreignKey:TicketID;constraint:OnDelete:RESTRICT"`
	OrderID          uint            `gorm:"not null;index"`
	AmountMinor      int64           `gorm:"not null"`
	Currency         string          `gorm:"size:16;not null"`
	Disposition      string          `gorm:"size:32;not null"`
	WalletEntryID    *uint           `gorm:"index"`
	CreatedAt        time.Time
	ProcessedAt      time.Time
	Order            *Order       `gorm:"foreignKey:OrderID;constraint:OnDelete:RESTRICT"`
	WalletEntry      *WalletEntry `gorm:"foreignKey:WalletEntryID;constraint:OnDelete:RESTRICT"`
}
