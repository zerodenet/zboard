package meteringstore

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func drainFlowDetailPolicy(t *testing.T, s FlowDetailRetention) {
	t.Helper()
	for i := 0; i < 30; i++ {
		ready, err := s.HasPending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !ready {
			return
		}
		if _, err := s.Drain(t.Context(), 200); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("flow retention did not converge")
}

func TestFlowDetailPolicyFortyDaysHasFiniteDiagnosticsAndMinuteHistory(t *testing.T) {
	db := trafficAggregateDB(t)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now := base
	policy := FlowDetailRetention{DB: db, Now: func() time.Time { return now }}
	for day := 0; day < 40; day++ {
		at := base.AddDate(0, 0, day)
		for i := 0; i < 20; i++ {
			flowStatisticsFixtureSource(t, db, 1, 7, fmt.Sprintf("%d-%d", day, i), at, at, true)
		}
		drainFlowStatistics(t, db, 200)
		now = at.Add(time.Hour)
		drainFlowDetailPolicy(t, policy)
	}
	for table, want := range map[string]int64{"subscription_flow_start_events": 20, "flow_start_statistics_points": 20, "flow_start_statistics_minutes": 30, "flow_start_statistics_pending": 0} {
		if n := statisticsCount(t, db, table); n != want {
			t.Fatalf("%s=%d want %d", table, n, want)
		}
	}
	spec, _ := metering.ParseObservationRange("15d")
	rows, nodes, err := flowStatisticsSeries(db, 7, spec, now)
	if err != nil || nodes != 1 {
		t.Fatal(nodes, err)
	}
	var total int64
	for _, r := range rows {
		total += r.ConnectionStarts
	}
	if total != 300 {
		t.Fatal("raw retirement erased minute history", total)
	}
	for _, predicate := range []string{"<", ">"} {
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN SELECT * FROM flow_start_statistics_minutes WHERE minute_at "+predicate+" ? ORDER BY minute_at,subscription_id,node_id,basis LIMIT 200", now).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		var detail string
		for _, p := range plan {
			detail += p.Detail + "\n"
		}
		if !strings.Contains(detail, "idx_flow_stats_minute_retention") || strings.Contains(strings.ToUpper(detail), "TEMP B-TREE") {
			t.Fatal("expiry lost its bounded ordered index seek", detail)
		}
	}
	count, nodes, err := flowStatisticsWindow(db, 7, "received", now.Add(-time.Hour-time.Second), now)
	if err != nil || count != 20 || nodes != 1 {
		t.Fatal("current exact policy changed", count, nodes, err)
	}
	t.Log("40 daily bursts: 20 raw + 20 exact points + 30 minute rows; 300 retained starts")
}

func TestFlowDetailUpgradeConvertsDeadlinesAndPreservesMinuteAndFinancialFacts(t *testing.T) {
	db := trafficAggregateDB(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	at := now.Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		flowStatisticsFixtureSource(t, db, 1, 7, fmt.Sprint(i), at, at, true)
	}
	drainFlowStatistics(t, db, 200)
	if err := db.Model(&model.FlowStartStatisticsPoint{}).Where("1=1").Update("review_at", at.Add(metering.ObservationRetentionHorizon)).Error; err != nil {
		t.Fatal(err)
	}
	// Financial authority is intentionally independent of diagnostic retention.
	ledger := model.TrafficRecord{NodeID: 1, ReportID: "financial-original", Nonce: "financial", RawBytes: 99, UsedBytes: 99, At: at}
	if err := db.Create(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	policy := FlowDetailRetention{DB: db, Now: func() time.Time { return now }}
	if n, err := policy.Drain(t.Context(), 2); err != nil || n != 4 {
		t.Fatal("legacy conversion was not bounded", n, err)
	}
	if statisticsCount(t, db, "subscription_flow_start_events") != 1 {
		t.Fatal("detail retirement waited for the entire legacy conversion")
	}
	var state model.FlowDetailRetentionState
	if err := db.First(&state, 1).Error; err != nil || state.AfterID != 2 {
		t.Fatal(state, err)
	}
	drainFlowDetailPolicy(t, policy)
	if statisticsCount(t, db, "subscription_flow_start_events") != 0 || statisticsCount(t, db, "flow_start_statistics_points") != 0 || statisticsCount(t, db, "flow_start_statistics_minutes") != 2 {
		t.Fatal("detail/history lifetimes remain coupled")
	}
	var got model.TrafficRecord
	if err := db.First(&got, ledger.ID).Error; err != nil || got.UsedBytes != 99 {
		t.Fatal("diagnostic expiry changed billing", got, err)
	}
	// A very old event is still accepted with today's received time, after the
	// earlier diagnostic identity expired. It remains visible on received basis.
	flowStatisticsFixtureSource(t, db, 1, 7, "0", at.AddDate(-20, 0, 0), now, true)
	drainFlowStatistics(t, db, 200)
	drainFlowDetailPolicy(t, policy)
	count, _, err := flowStatisticsWindow(db, 7, "received", now.Add(-time.Minute), now)
	if err != nil || count != 1 {
		t.Fatal("historical observation admission stopped", count, err)
	}
	// Bad node clocks cannot plant never-expiring future minute keys. Admission
	// and trusted received-time diagnostics still work, without touching billing.
	future := now.AddDate(20, 0, 0).Truncate(time.Minute)
	flowStatisticsFixtureSource(t, db, 1, 7, "future-clock", future, now, true)
	drainFlowStatistics(t, db, 200)
	var futureRows int64
	if err := db.Model(&model.FlowStartStatisticsMinute{}).Where("minute_at = ?", future).Count(&futureRows).Error; err != nil || futureRows != 0 {
		t.Fatal("future clock bypassed finite bucket policy", futureRows, err)
	}
	if err := db.Create(&model.FlowStartStatisticsMinute{SubscriptionID: 7, NodeID: 99, Basis: "occurred", MinuteAt: future, EventCount: 1}).Error; err != nil {
		t.Fatal(err)
	}
	drainFlowDetailPolicy(t, policy)
	if err := db.Model(&model.FlowStartStatisticsMinute{}).Where("minute_at = ?", future).Count(&futureRows).Error; err != nil || futureRows != 0 {
		t.Fatal("old future bucket never expired", futureRows, err)
	}
	count, _, err = flowStatisticsWindow(db, 7, "received", now.Add(-time.Minute), now)
	if err != nil || count != 2 {
		t.Fatal("bad clock discarded received-time observation", count, err)
	}
}
