package model

// MeteringIdentityFence protects hot/cold identity transfers, independently of
// mutable node administration and heartbeat rows. One permanent row per source
// node; no per-report rows and no FK to a deletable management asset.
type MeteringIdentityFence struct {
	NodeID uint `gorm:"primaryKey;autoIncrement:false"`
}
