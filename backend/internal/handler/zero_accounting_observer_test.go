package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

func TestAccountingBenchmarkObserverCountsOnlyNumericEvidence(t *testing.T) {
	counter := &accountingQueryCounter{Interface: logger.Discard}
	for _, item := range []struct {
		sql  string
		rows int64
		err  error
	}{
		{"SELECT secret FROM private", 9, nil},
		{"insert INTO private VALUES ('never retain this')", 3, nil},
		{" UPDATE private SET value = 'private'", 2, nil},
		{"DELETE FROM private", -1, errors.New("test failure")},
		{"PRAGMA journal_mode", 1, nil},
	} {
		counter.Trace(context.Background(), time.Now().Add(-time.Millisecond), func() (string, int64) { return item.sql, item.rows }, item.err)
	}
	if counter.queries.Load() != 5 || counter.reads.Load() != 1 || counter.writes.Load() != 3 || counter.rows.Load() != 5 || counter.unknown.Load() != 1 || counter.errors.Load() != 1 {
		t.Fatal("SQL evidence conflated selected rows, unknown affected rows, and DML", counter)
	}
	var samples uint64
	for i := range counter.latency {
		samples += counter.latency[i].Load()
	}
	if samples != 5 || counter.totalNS.Load() == 0 || counter.maxNS.Load() == 0 {
		t.Fatal("missing bounded client latency evidence")
	}
}
