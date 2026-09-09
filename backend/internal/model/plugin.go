package model

import "time"

// PluginInstallation stores host-owned desired state. Plugins never receive a DB handle.
type PluginInstallation struct {
	LocalTrust       bool      `gorm:"not null;default:false" json:"local_trust"`
	SigningKey       string    `gorm:"size:64;not null;default:''" json:"signing_key"`
	ID               string    `gorm:"primaryKey;size:160" json:"id"`
	VersionID        string    `gorm:"size:64;not null" json:"version_id"`
	Name             string    `gorm:"size:160;not null" json:"name"`
	Publisher        string    `gorm:"size:160;not null" json:"publisher"`
	State            string    `gorm:"size:24;not null" json:"state"`
	Enabled          bool      `gorm:"not null" json:"enabled"`
	Generation       uint64    `gorm:"not null" json:"generation"`
	ConfigRevision   uint64    `gorm:"not null" json:"config_revision"`
	ConfigCiphertext string    `gorm:"type:text;not null" json:"-"`
	LastError        string    `gorm:"type:text;not null" json:"last_error"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type PluginVersion struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	PluginID  string    `gorm:"size:160;not null;index" json:"plugin_id"`
	Version   string    `gorm:"size:64;not null" json:"version"`
	Digest    string    `gorm:"size:64;not null" json:"digest"`
	Publisher string    `gorm:"size:160;not null" json:"publisher"`
	Manifest  string    `gorm:"type:text;not null" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type PluginOperation struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	PluginID  string    `gorm:"size:160;not null;index" json:"plugin_id"`
	Action    string    `gorm:"size:32;not null" json:"action"`
	State     string    `gorm:"size:24;not null" json:"state"`
	Actor     string    `gorm:"size:191;not null" json:"actor"`
	Message   string    `gorm:"type:text;not null" json:"message"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PluginHostLease struct {
	ID        uint      `gorm:"primaryKey"`
	Owner     string    `gorm:"size:64;not null"`
	Epoch     uint64    `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null"`
}
