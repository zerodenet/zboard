package model

import "time"

// Independent of retained mail tasks: pruning delivery history must not make
// the same subscription episode eligible for another notification.
type SubscriptionAlert struct {
	ID             uint       `gorm:"primaryKey"`
	AlertKey       string     `gorm:"size:64;uniqueIndex;not null"`
	UserID         uint       `gorm:"index:idx_subscription_alert_user_time,priority:1;not null"`
	SubscriptionID uint       `gorm:"index;not null"`
	Kind           string     `gorm:"size:24;not null"`
	Episode        string     `gorm:"size:191;not null"`
	TaskID         uint       `gorm:"index;not null"`
	CreatedAt      time.Time  `gorm:"index:idx_subscription_alert_user_time,priority:2;not null"`
	AttemptedAt    *time.Time `gorm:"index"`
}

type SubscriptionAlertScan struct {
	ID                 uint `gorm:"primaryKey"`
	LastSubscriptionID uint `gorm:"not null;default:0"`
}
