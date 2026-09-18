package meteringstore

import (
	"gorm.io/gorm"
	"sync/atomic"
	"time"
)

var fairUseRawCleanupNextUnixMilli atomic.Int64

// AfterCreate performs opportunistic, process-throttled retention. At high
// event volume the atomic fast path avoids a database query for almost every
// event; cleanup is deliberately best-effort so Fair Use storage maintenance
// cannot make Zero event delivery or traffic accounting fail.
func (row *FlowStartRecord) AfterCreate(tx *gorm.DB) error {
	now := row.ReceivedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return RunRetentionCleanup(
		tx,
		&fairUseRawCleanupNextUnixMilli,
		now,
		func(tx *gorm.DB, cutoff time.Time) *gorm.DB {
			return tx.Where("received_at < ?", cutoff).Delete(&FlowStartRecord{})
		},
		now.Add(-15*24*time.Hour),
		"raw activity",
	)
}
