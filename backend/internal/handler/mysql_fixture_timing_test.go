package handler

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// Optional test diagnostics only. No SQL, permissions, pool sizing, fixture
// source/DDL, or test budgets change. Rows are cumulative per-process spans,
// not isolated test attribution; install_total overlaps its create/seed parts.
var mysqlFixturePreparationPhases = [7]string{"prepare/versioned", "prepare/commerce", "prepare/subscription_access", "prepare/zero_event", "prepare/traffic_read", "prepare/operations", "prepare/fair_use"}
var mysqlFixtureTimingPhases = map[string]bool{"create_schema": true, "open_pool": true, "close_pool": true, "drop_schema": true, "services": true, "capture": true, "install_total": true, "install_create_table": true, "install_seed_tx": true, "prepare/versioned": true, "prepare/commerce": true, "prepare/subscription_access": true, "prepare/zero_event": true, "prepare/traffic_read": true, "prepare/operations": true, "prepare/fair_use": true}

type mysqlFixtureTimingRow struct {
	Kind        string `json:"kind"`
	Phase       string `json:"phase"`
	Samples     uint64 `json:"samples"`
	Errors      uint64 `json:"errors"`
	WallTotalNS int64  `json:"wall_total_ns"`
	WallMaxNS   int64  `json:"wall_max_ns"`
}
type mysqlFixtureTimingLedger struct {
	sync.Mutex
	rows map[[2]string]mysqlFixtureTimingRow
}

func (l *mysqlFixtureTimingLedger) record(kind, phase string, elapsed time.Duration, err error) {
	if (kind != "publish" && kind != "business" && kind != "business_template") || !mysqlFixtureTimingPhases[phase] || elapsed < 0 {
		return
	}
	l.Lock()
	defer l.Unlock()
	if l.rows == nil {
		l.rows = map[[2]string]mysqlFixtureTimingRow{}
	}
	key := [2]string{kind, phase}
	row := l.rows[key]
	row.Kind, row.Phase = kind, phase
	row.Samples++
	if err != nil {
		row.Errors++
	}
	row.WallTotalNS += elapsed.Nanoseconds()
	if elapsed.Nanoseconds() > row.WallMaxNS {
		row.WallMaxNS = elapsed.Nanoseconds()
	}
	l.rows[key] = row
}
func (l *mysqlFixtureTimingLedger) snapshot() []mysqlFixtureTimingRow {
	l.Lock()
	defer l.Unlock()
	rows := make([]mysqlFixtureTimingRow, 0, len(l.rows))
	for _, row := range l.rows {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Phase < rows[j].Phase
	})
	return rows
}

var mysqlFixtureTimings mysqlFixtureTimingLedger
var mysqlFixtureTimingReporters sync.Map

func beginMySQLFixtureTiming(kind, phase string) func(error) {
	started := time.Now()
	var once sync.Once
	return func(err error) { once.Do(func() { mysqlFixtureTimings.record(kind, phase, time.Since(started), err) }) }
}
func shouldReportMySQLFixtureTiming(failed bool, optIn string) bool { return failed || optIn == "1" }
func reportMySQLFixtureTimingOnCleanup(t testing.TB) {
	t.Helper()
	if _, loaded := mysqlFixtureTimingReporters.LoadOrStore(t, struct{}{}); loaded {
		return
	}
	// Registered before fixture cleanups, hence emitted after their Close/DROP.
	t.Cleanup(func() {
		defer mysqlFixtureTimingReporters.Delete(t)
		if t.Skipped() || !shouldReportMySQLFixtureTiming(t.Failed(), os.Getenv("ZBOARD_TEST_MYSQL_FIXTURE_TIMING")) {
			return
		}
		report := struct {
			Scope   string                  `json:"scope"`
			Overlap string                  `json:"overlap"`
			Rows    []mysqlFixtureTimingRow `json:"rows"`
		}{"cumulative_process", "install_total includes install_create_table and install_seed_tx; do not sum parent and child", mysqlFixtureTimings.snapshot()}
		raw, err := json.Marshal(report)
		if err != nil {
			t.Log("MYSQL_FIXTURE_TIMING unavailable")
			return
		}
		t.Logf("MYSQL_FIXTURE_TIMING %s", raw)
	})
}
