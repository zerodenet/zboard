package model

import "time"

// ExternalIdentity belongs to core authentication, never to plugin storage.
// ID is a hash of publisher, plugin, issuer and subject to retain byte-exact
// identity matching even on databases configured with case-insensitive collation.
type ExternalIdentity struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_external_identity_user_plugin" json:"-"`
	PluginID  string    `gorm:"not null;size:160;uniqueIndex:idx_external_identity_user_plugin" json:"plugin_id"`
	Publisher string    `gorm:"not null;size:160" json:"-"`
	Issuer    string    `gorm:"not null;type:text" json:"issuer"`
	Subject   string    `gorm:"not null;type:text" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
