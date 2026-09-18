package meteringstore

import (
	"gorm.io/gorm"
	"log"
	"sync/atomic"
	"time"
)

var eventCleanupNext atomic.Int64

// Evaluation events are derived, reproducible analysis artifacts rather than
// durable business/audit facts. Keep them on the same fifteen-day horizon as
// the raw observations used to explain them.
func (row *EventRecord) AfterCreate(tx *gorm.DB) error {
	now := row.OccurredAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return RunRetentionCleanup(
		tx,
		&eventCleanupNext,
		now,
		func(tx *gorm.DB, cutoff time.Time) *gorm.DB {
			return tx.Where("occurred_at < ?", cutoff).Delete(&EventRecord{})
		},
		now.Add(-15*24*time.Hour),
		"evaluation event",
	)
}

func RunRetentionCleanup(
	tx *gorm.DB,
	nextCleanup *atomic.Int64,
	now time.Time,
	cleanup func(*gorm.DB, time.Time) *gorm.DB,
	cutoff time.Time,
	label string,
) error {
	nowMillis := now.UnixMilli()
	for {
		next := nextCleanup.Load()
		if next > nowMillis {
			return nil
		}
		if !nextCleanup.CompareAndSwap(next, now.Add(10*time.Minute).UnixMilli()) {
			continue
		}
		result := cleanup(tx, cutoff)
		if result.Error != nil {
			nextCleanup.Store(now.Add(time.Minute).UnixMilli())
			log.Printf("fair use %s retention cleanup failed: %v", label, result.Error)
			return nil
		}
		if result.RowsAffected > 0 {
			log.Printf("fair use %s retention cleanup completed: deleted=%d", label, result.RowsAffected)
		}
		return nil
	}
}
