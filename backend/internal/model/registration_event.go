package model

import "time"

// AccountRegistrationEvent is the identity-owned durable handoff. It contains no
// credential or message body and is inserted in the account creation transaction.
type AccountRegistrationEvent struct {
	AccountID   uint       `gorm:"primaryKey;autoIncrement:false"`
	OccurredAt  time.Time  `gorm:"not null"`
	ProcessedAt *time.Time `gorm:"index"`
	TaskID      *uint
}
