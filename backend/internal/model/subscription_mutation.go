package model

import "time"

// SubscriptionMutation is the durable entitlement command/replay record.
type SubscriptionMutation struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	SubscriptionID uint      `json:"subscription_id" gorm:"index;not null"`
	ActorID        uint      `json:"actor_id" gorm:"not null"`
	Kind           string    `json:"kind" gorm:"size:32;not null"`
	Days           int       `json:"days" gorm:"not null;default:0"`
	Reason         string    `json:"reason" gorm:"size:255;not null"`
	Origin         string    `json:"origin" gorm:"size:191;not null;default:''"`
	IdempotencyKey string    `json:"idempotency_key" gorm:"size:128;uniqueIndex;not null"`
	RequestHash    string    `json:"request_hash" gorm:"size:64;not null"`
	Status         string    `json:"status" gorm:"size:20;not null"`
	EndAt          time.Time `json:"end_at" gorm:"not null"`
	CreatedAt      time.Time `json:"created_at"`
}
