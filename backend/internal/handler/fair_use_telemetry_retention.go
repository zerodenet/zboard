package handler

import (
	"log"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

const (
	// The public metrics endpoint accepts at most a one-hour window. Keep one
	// extra hour of raw activity for late delivery/skew diagnostics, but do not
	// turn per-flow starts into permanent history. Long-lived Fair Use history
	// belongs in later aggregate/evaluation event tables.
	fairUseRawActivityRetention = 2 * time.Hour
	fairUseRawCleanupInterval   = 10 * time.Minute
	fairUseRawCleanupRetry      = time.Minute
)

var fairUseRawCleanupNextUnixMilli atomic.Int64

func fairUseRawActivityCutoff(now time.Time) time.Time {
	return now.UTC().Add(-fairUseRawActivityRetention)
}

// AfterCreate performs opportunistic, process-throttled retention. At high
// event volume the atomic fast path avoids a database query for almost every
// event; at low volume stale rows are harmless and are pruned by the next
// accepted flow start. Cleanup is deliberately best-effort so Fair Use storage
// maintenance cannot make Zero event delivery or traffic accounting fail.
func (row *subscriptionFlowStartEvent) AfterCreate(tx *gorm.DB) error {
	now := row.ReceivedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowMillis := now.UnixMilli()
	for {
		next := fairUseRawCleanupNextUnixMilli.Load()
		if next > nowMillis {
			return nil
		}
		if !fairUseRawCleanupNextUnixMilli.CompareAndSwap(next, now.Add(fairUseRawCleanupInterval).UnixMilli()) {
			continue
		}
		result := tx.Where("received_at < ?", fairUseRawActivityCutoff(now)).Delete(&subscriptionFlowStartEvent{})
		if result.Error != nil {
			fairUseRawCleanupNextUnixMilli.Store(now.Add(fairUseRawCleanupRetry).UnixMilli())
			log.Printf("fair use raw activity retention cleanup failed: %v", result.Error)
			return nil
		}
		if result.RowsAffected > 0 {
			log.Printf("fair use raw activity retention cleanup completed: deleted=%d", result.RowsAffected)
		}
		return nil
	}
}
