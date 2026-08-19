package handler

import (
	"log"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

const (
	// Fair Use is currently a data-collection and analysis feature. Keep the
	// underlying observation facts long enough to compare real user behaviour,
	// but never turn per-flow activity or derived evaluation events into
	// permanent history. Fifteen days is the maximum observation horizon.
	fairUseObservationRetention = 15 * 24 * time.Hour
	fairUseRawCleanupInterval   = 10 * time.Minute
	fairUseRawCleanupRetry      = time.Minute
)

var fairUseRawCleanupNextUnixMilli atomic.Int64
var fairUseEventCleanupNextUnixMilli atomic.Int64

func fairUseRawActivityCutoff(now time.Time) time.Time {
	return now.UTC().Add(-fairUseObservationRetention)
}

func fairUseEvaluationEventCutoff(now time.Time) time.Time {
	return now.UTC().Add(-fairUseObservationRetention)
}

// AfterCreate performs opportunistic, process-throttled retention. At high
// event volume the atomic fast path avoids a database query for almost every
// event; cleanup is deliberately best-effort so Fair Use storage maintenance
// cannot make Zero event delivery or traffic accounting fail.
func (row *subscriptionFlowStartEvent) AfterCreate(tx *gorm.DB) error {
	now := row.ReceivedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return runFairUseRetentionCleanup(
		tx,
		&fairUseRawCleanupNextUnixMilli,
		now,
		func(tx *gorm.DB, cutoff time.Time) *gorm.DB {
			return tx.Where("received_at < ?", cutoff).Delete(&subscriptionFlowStartEvent{})
		},
		fairUseRawActivityCutoff(now),
		"raw activity",
	)
}

// Evaluation events are derived, reproducible analysis artifacts rather than
// durable business/audit facts. Keep them on the same fifteen-day horizon as
// the raw observations used to explain them.
func (row *subscriptionFairUseEvent) AfterCreate(tx *gorm.DB) error {
	now := row.OccurredAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return runFairUseRetentionCleanup(
		tx,
		&fairUseEventCleanupNextUnixMilli,
		now,
		func(tx *gorm.DB, cutoff time.Time) *gorm.DB {
			return tx.Where("occurred_at < ?", cutoff).Delete(&subscriptionFairUseEvent{})
		},
		fairUseEvaluationEventCutoff(now),
		"evaluation event",
	)
}

func runFairUseRetentionCleanup(
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
		if !nextCleanup.CompareAndSwap(next, now.Add(fairUseRawCleanupInterval).UnixMilli()) {
			continue
		}
		result := cleanup(tx, cutoff)
		if result.Error != nil {
			nextCleanup.Store(now.Add(fairUseRawCleanupRetry).UnixMilli())
			log.Printf("fair use %s retention cleanup failed: %v", label, result.Error)
			return nil
		}
		if result.RowsAffected > 0 {
			log.Printf("fair use %s retention cleanup completed: deleted=%d", label, result.RowsAffected)
		}
		return nil
	}
}
