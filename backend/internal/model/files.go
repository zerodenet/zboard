package model

import "time"

type StoredFile struct {
	ID          string    `gorm:"primaryKey;size:36"`
	OwnerID     uint      `gorm:"not null;index"`
	Purpose     string    `gorm:"size:16;not null"`
	Name        string    `gorm:"size:255;not null"`
	ContentType string    `gorm:"size:80;not null"`
	Size        int64     `gorm:"not null"`
	CreatedAt   time.Time `gorm:"not null"`
	DeletedAt   *time.Time
}

type TicketAttachment struct {
	ID        uint    `gorm:"primaryKey"`
	MessageID uint    `gorm:"not null;index"`
	FileID    *string `gorm:"size:36;uniqueIndex"`
	Name      string  `gorm:"size:255;not null"`
	URL       string  `gorm:"size:2048;not null"`
}
