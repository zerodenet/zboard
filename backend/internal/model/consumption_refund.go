package model

import "time"

// ConsumptionRefund records an approved wallet refund against one original
// purchase. It consumes OrderValueBudget; it is not a top-up or an external payout.
type ConsumptionRefund struct {
	ID          uint   `gorm:"primaryKey;index:idx_consumption_refund_order,priority:2"`
	RequestKey  string `gorm:"size:64;not null;uniqueIndex"`
	Fingerprint string `gorm:"size:64;not null"`
	OrderID     uint   `gorm:"not null;index:idx_consumption_refund_order,priority:1"`
	UserID      uint   `gorm:"not null;index"`
	Currency    string `gorm:"size:16;not null"`
	AmountMinor int64  `gorm:"not null;check:ck_consumption_refund_amount,amount_minor > 0"`
	TicketID    uint   `gorm:"not null;index"`
	ActorID     uint   `gorm:"not null;index"`
	Reason      string `gorm:"size:400;not null"`
	EntryID     uint   `gorm:"not null;uniqueIndex"`
	CreatedAt   time.Time
	Order       *Order            `gorm:"foreignKey:OrderID;constraint:OnDelete:RESTRICT"`
	Budget      *OrderValueBudget `gorm:"belongsTo:OrderValueBudget;foreignKey:OrderID;references:OrderID;constraint:OnDelete:RESTRICT"`
	User        *AccountSubject   `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
	Actor       *AccountSubject   `gorm:"foreignKey:ActorID;constraint:OnDelete:RESTRICT"`
	Ticket      *Ticket           `gorm:"foreignKey:TicketID;constraint:OnDelete:RESTRICT"`
	Entry       *WalletEntry      `gorm:"foreignKey:EntryID;constraint:OnDelete:RESTRICT"`
}
