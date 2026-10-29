package model

import "time"

// Wallet balances and ledger entries are currency minor units, never traffic.
// Financial evidence restricts hard deletion of its owner and source order.
type WalletAccount struct {
	UserID        uint      `json:"-" gorm:"primaryKey;autoIncrement:false;not null"`
	Currency      string    `json:"currency" gorm:"primaryKey;size:16;not null"`
	BalanceMinor  int64     `json:"balance_minor" gorm:"not null;default:0;check:ck_wallet_available,balance_minor >= 0 AND reserved_minor >= 0 AND reserved_minor <= balance_minor"`
	ReservedMinor int64     `json:"reserved_minor" gorm:"not null;default:0"`
	Revision      uint64    `json:"revision" gorm:"not null;default:1"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	User          *User     `json:"-" gorm:"foreignKey:UserID;constraint:OnDelete:RESTRICT"`
}

type WalletEntry struct {
	ID             uint           `json:"id" gorm:"primaryKey;index:idx_wallet_entries_account,priority:3"`
	EntryKey       string         `json:"-" gorm:"size:64;not null;uniqueIndex"`
	UserID         uint           `json:"-" gorm:"not null;index:idx_wallet_entries_account,priority:1"`
	Currency       string         `json:"currency" gorm:"size:16;not null;index:idx_wallet_entries_account,priority:2"`
	Kind           string         `json:"kind" gorm:"size:32;not null"`
	BalanceDelta   int64          `json:"balance_delta" gorm:"not null"`
	ReservedDelta  int64          `json:"reserved_delta" gorm:"not null;default:0"`
	BalanceBefore  int64          `json:"balance_before" gorm:"not null"`
	BalanceAfter   int64          `json:"balance_after" gorm:"not null"`
	ReservedBefore int64          `json:"reserved_before" gorm:"not null;default:0"`
	ReservedAfter  int64          `json:"reserved_after" gorm:"not null;default:0"`
	SourceOrderID  *uint          `json:"source_order_id,omitempty" gorm:"index"`
	Reason         string         `json:"reason" gorm:"size:500;not null"`
	CreatedAt      time.Time      `json:"created_at"`
	Account        *WalletAccount `json:"-" gorm:"belongsTo:WalletAccount;foreignKey:UserID,Currency;references:UserID,Currency;constraint:OnDelete:RESTRICT"`
	SourceOrder    *Order         `json:"-" gorm:"foreignKey:SourceOrderID;constraint:OnDelete:RESTRICT"`
}
