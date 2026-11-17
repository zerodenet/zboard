package model

import "time"

// A controlled debit is an internal ledger fact; no bank transfer is executed.
type WalletAdjustment struct {
	ID              uint   `gorm:"primaryKey;index:idx_wallet_adjustment_owner,priority:3"`
	RequestKey      string `gorm:"size:64;not null;uniqueIndex"`
	Fingerprint     string `gorm:"size:64;not null"`
	UserID          uint   `gorm:"not null;index:idx_wallet_adjustment_owner,priority:1"`
	Currency        string `gorm:"size:16;not null;index:idx_wallet_adjustment_owner,priority:2"`
	AmountMinor     int64  `gorm:"not null;check:ck_wallet_adjustment_amount,amount_minor > 0"`
	Reason          string `gorm:"size:400;not null"`
	TicketID        uint   `gorm:"not null;index"`
	ActorID         uint   `gorm:"not null;index"`
	EntryID         uint   `gorm:"not null;uniqueIndex"`
	ReversalEntryID *uint  `gorm:"uniqueIndex"`
	ReversalActorID *uint  `gorm:"index"`
	ReversalReason  string `gorm:"size:400;not null;default:''"`
	ExternalStatus  string `gorm:"size:24;not null;default:pending"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	User            *AccountSubject `gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
	Actor           *AccountSubject `gorm:"foreignKey:ActorID;constraint:OnDelete:RESTRICT"`
	ReversalActor   *AccountSubject `gorm:"foreignKey:ReversalActorID;constraint:OnDelete:RESTRICT"`
	Ticket          *Ticket         `gorm:"foreignKey:TicketID;constraint:OnDelete:RESTRICT"`
	Entry           *WalletEntry    `gorm:"foreignKey:EntryID;constraint:OnDelete:RESTRICT"`
	ReversalEntry   *WalletEntry    `gorm:"foreignKey:ReversalEntryID;constraint:OnDelete:RESTRICT"`
}
type WalletPayoutResult struct {
	ID           uint      `gorm:"primaryKey;index:idx_wallet_payout_history,priority:2"`
	AdjustmentID uint      `gorm:"not null;index:idx_wallet_payout_history,priority:1"`
	RequestKey   string    `gorm:"size:64;not null;uniqueIndex"`
	Fingerprint  string    `gorm:"size:64;not null"`
	ActorID      uint      `gorm:"not null;index"`
	Outcome      string    `gorm:"size:24;not null"`
	Reference    string    `gorm:"size:128;not null;default:''"`
	Note         string    `gorm:"size:500;not null;default:''"`
	OccurredAt   time.Time `gorm:"not null"`
	CreatedAt    time.Time
	Adjustment   *WalletAdjustment `gorm:"foreignKey:AdjustmentID;constraint:OnDelete:RESTRICT"`
	Actor        *AccountSubject   `gorm:"foreignKey:ActorID;constraint:OnDelete:RESTRICT"`
}
