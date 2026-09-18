package model

import "time"

// MailDeliveryAttempt preserves channel evidence independently of retry and review.
type MailDeliveryAttempt struct {
	ID         uint      `gorm:"primaryKey"`
	TaskID     uint      `gorm:"not null;index"`
	ItemID     uint      `gorm:"not null;uniqueIndex:mail_attempt_item_number"`
	Attempt    int       `gorm:"not null;uniqueIndex:mail_attempt_item_number"`
	LeaseToken string    `gorm:"size:191;not null"`
	Acceptance string    `gorm:"size:24;not null"`
	StartedAt  time.Time `gorm:"not null"`
	FinishedAt *time.Time
}
