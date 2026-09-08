package model

import "time"

// Authorization is bound to a reviewed package, not just a publisher or plugin ID.
type PluginAuthorization struct {
	PluginID      string    `gorm:"primaryKey;size:160" json:"-"`
	Digest        string    `gorm:"size:64;not null" json:"digest"`
	Capabilities  string    `gorm:"type:text;not null" json:"-"`
	NativeTrusted bool      `gorm:"not null" json:"native_trusted"`
	Actor         string    `gorm:"size:191;not null" json:"actor"`
	UpdatedAt     time.Time `json:"updated_at"`
}
type PluginData struct {
	PluginID   string    `gorm:"primaryKey;size:160" json:"-"`
	Epoch      uint64    `gorm:"not null" json:"epoch"`
	Version    uint64    `gorm:"not null" json:"version"`
	Revision   uint64    `gorm:"not null" json:"revision"`
	Ciphertext string    `gorm:"type:text;not null" json:"-"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type PluginMigration struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	PluginID  string    `gorm:"size:160;not null;index" json:"-"`
	Epoch     uint64    `gorm:"not null" json:"epoch"`
	Version   uint64    `gorm:"not null" json:"version"`
	Checksum  string    `gorm:"size:64;not null" json:"checksum"`
	Digest    string    `gorm:"size:64;not null" json:"digest"`
	Actor     string    `gorm:"size:191;not null" json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}
