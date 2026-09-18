package handler

import (
	"time"
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

func fairUseRawActivityCutoff(now time.Time) time.Time {
	return now.UTC().Add(-fairUseObservationRetention)
}

func fairUseEvaluationEventCutoff(now time.Time) time.Time {
	return now.UTC().Add(-fairUseObservationRetention)
}
