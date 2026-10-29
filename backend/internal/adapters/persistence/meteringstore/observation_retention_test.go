package meteringstore

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func observationRetentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "observations.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Logger = logger.Discard
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestObservationRetentionSQLiteBatchBoundaryAndRepeat(t *testing.T) {
	db := observationRetentionDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-metering.ObservationRetentionHorizon)
	raw := make([]FlowStartRecord, 1005)
	events := make([]EventRecord, 1005)
	for i := range raw {
		raw[i] = FlowStartRecord{NodeID: 1, EventID: fmt.Sprint(i), ReceivedAt: cutoff.Add(-time.Millisecond), OccurredAt: now, CreatedAt: now}
		events[i] = EventRecord{OccurredAt: cutoff.Add(-time.Millisecond), CreatedAt: now}
	}
	if err := db.CreateInBatches(&raw, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&events, 100).Error; err != nil {
		t.Fatal(err)
	}
	for index, at := range []time.Time{cutoff, cutoff.Add(time.Millisecond), now} {
		// Raw retention uses received_at, not the old event occurrence time.
		if err := db.Create(&FlowStartRecord{NodeID: 1, EventID: fmt.Sprint("boundary-", index), ReceivedAt: at, OccurredAt: cutoff.Add(-time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&EventRecord{OccurredAt: at, CreatedAt: cutoff.Add(-time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	projectRetentionFixture(t, db)
	service := metering.ObservationRetention{Repository: ObservationRetention{DB: db}}
	for i, want := range []int64{1000, 5, 0} {
		out, err := service.Run(context.Background(), now)
		if err != nil || out.FlowStarts != want || out.EvaluationEvents != want {
			t.Fatalf("round %d: %+v %v", i, out, err)
		}
	}
	for _, model := range []any{&FlowStartRecord{}, &EventRecord{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 3 {
			t.Fatalf("boundary/recent data lost in %T: %d %v", model, count, err)
		}
	}
}

func TestObservationRetentionSQLiteRechecksCutoffAfterUnlockedDiscovery(t *testing.T) {
	db := observationRetentionDB(t)
	cutoff := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	rows := []FlowStartRecord{retentionFlow(cutoff.Add(-time.Hour), "first"), retentionFlow(cutoff.Add(-time.Hour), "second")}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	projectRetentionFixture(t, db)
	discovered, locked := 0, 0
	if err := db.Callback().Query().After("gorm:query").Register("test:refresh_discovered_observation", func(tx *gorm.DB) {
		if tx.Statement.Table != (FlowStartRecord{}).TableName() && tx.Statement.Table != "flow_start_statistics_points" {
			return
		}
		_, inTransaction := tx.Statement.ConnPool.(*sql.Tx)
		_, hasLock := tx.Statement.Clauses["FOR"]
		query := tx.Statement.SQL.String()
		if tx.Statement.Table == "flow_start_statistics_points" && strings.Contains(query, "review_at") && strings.Contains(query, "received_at<") {
			discovered++
			if inTransaction || hasLock {
				t.Error("candidate range read acquired a write transaction/lock")
			}
			if err := db.Exec("UPDATE subscription_flow_start_events SET received_at = ? WHERE id = ?", cutoff, rows[0].ID).Error; err != nil {
				t.Error(err)
			}
		} else if tx.Statement.Table == (FlowStartRecord{}).TableName() && strings.Contains(query, "id IN") {
			locked++
			if !inTransaction || !hasLock || strings.Contains(query, "received_at <") {
				t.Error("revalidation is not an exact-primary-key locking read")
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	count, err := (ObservationRetention{DB: db}).PruneFlowStarts(context.Background(), cutoff, 2)
	if err != nil || count != 1 || discovered != 1 || locked != 1 {
		t.Fatalf("revalidation: count=%d err=%v discovery=%d locks=%d", count, err, discovered, locked)
	}
	var retained FlowStartRecord
	if err := db.First(&retained, rows[0].ID).Error; err != nil || !retained.ReceivedAt.Equal(cutoff) {
		t.Fatalf("refreshed candidate deleted: %+v %v", retained, err)
	}
}

func TestObservationRetentionSQLiteRollbackAndInsertIndependence(t *testing.T) {
	for _, table := range []string{(FlowStartRecord{}).TableName(), (EventRecord{}).TableName()} {
		t.Run(table, func(t *testing.T) {
			db := observationRetentionDB(t)
			cutoff := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
			var model any = &FlowStartRecord{}
			if table == (EventRecord{}).TableName() {
				model = &EventRecord{}
			}
			for i := 0; i < 3; i++ {
				source := retentionFlow(cutoff.Add(-time.Hour), fmt.Sprint("rollback-", i))
				var row any = &source
				if table == (EventRecord{}).TableName() {
					row = &EventRecord{OccurredAt: cutoff.Add(-time.Hour)}
				}
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			projectRetentionFixture(t, db)
			if err := db.Exec("CREATE TRIGGER reject_observation_delete BEFORE DELETE ON " + table + " WHEN OLD.id = 2 BEGIN SELECT RAISE(ABORT, 'maintenance unavailable'); END").Error; err != nil {
				t.Fatal(err)
			}
			deleteCalls := 0
			var deleteTables []string
			if err := db.Callback().Delete().Before("gorm:delete").Register("test:observe_observation_deletes", func(tx *gorm.DB) { deleteCalls++; deleteTables = append(deleteTables, tx.Statement.Table) }); err != nil {
				t.Fatal(err)
			}
			fresh := retentionFlow(cutoff.Add(time.Hour), "fresh")
			var received any = &fresh
			if table == (EventRecord{}).TableName() {
				received = &EventRecord{OccurredAt: cutoff.Add(time.Hour)}
			}
			if err := db.Create(received).Error; err != nil || deleteCalls != 0 {
				t.Fatalf("insertion attempted cleanup: %v deletes=%d", err, deleteCalls)
			}
			store := ObservationRetention{DB: db}
			prune := store.PruneFlowStarts
			if table == (EventRecord{}).TableName() {
				prune = store.PruneEvaluationEvents
			}
			count, err := prune(context.Background(), cutoff, 3)
			wantDeletes := 1
			if table == (FlowStartRecord{}).TableName() {
				wantDeletes = 4
			}
			if err == nil || count != 0 || deleteCalls != wantDeletes {
				t.Fatalf("failed maintenance acknowledged: %d %v calls=%d", count, err, deleteCalls)
			}

			if table == (FlowStartRecord{}).TableName() {
				if len(deleteTables) != 4 || deleteTables[0] != "flow_start_statistics_minutes" || deleteTables[1] != "flow_start_statistics_minutes" || deleteTables[2] != "flow_start_statistics_points" || deleteTables[3] != (FlowStartRecord{}).TableName() {
					t.Fatal("retention no longer verifies both transactional deletion paths", deleteTables)
				}
				var points, total int64
				if err := db.Table("flow_start_statistics_points").Count(&points).Error; err != nil || points != 3 {
					t.Fatal("failed raw delete lost points", points, err)
				}
				if err := db.Table("flow_start_statistics_minutes").Where("basis=?", "received").Select("COALESCE(SUM(event_count),0)").Scan(&total).Error; err != nil || total != 3 {
					t.Fatal("failed raw delete changed aggregate", total, err)
				}
			}
			var rows int64
			if err := db.Model(model).Count(&rows).Error; err != nil || rows != 4 {
				t.Fatalf("partial deletion/received fact lost: %d %v", rows, err)
			}
			for _, limit := range []int{0, -1, 1001} {
				if count, err := prune(context.Background(), cutoff, limit); err == nil || count != 0 {
					t.Fatalf("unbounded limit accepted: %d %d %v", limit, count, err)
				}
			}
		})
	}
}

func TestObservationRetentionSQLiteIndependentTableProgress(t *testing.T) {
	db := observationRetentionDB(t)
	now := time.Now().UTC()
	old := now.Add(-16 * 24 * time.Hour)
	source := retentionFlow(old, "independent")
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&EventRecord{OccurredAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	projectRetentionFixture(t, db)
	if err := db.Exec("CREATE TRIGGER reject_raw_retention BEFORE DELETE ON subscription_flow_start_events BEGIN SELECT RAISE(ABORT, 'raw unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	out, err := (metering.ObservationRetention{Repository: ObservationRetention{DB: db}}).Run(context.Background(), now)
	if err == nil || out.FlowStarts != 0 || out.EvaluationEvents != 1 {
		t.Fatalf("second independent table did not commit: %+v %v", out, err)
	}
}

func TestObservationRetentionSQLiteDiscoveryUsesExistingRawTimeIndex(t *testing.T) {
	db, err := datastore.OpenWithDriver(datastore.DriverSQLite, filepath.Join(t.TempDir(), "legacy-observation-index.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&FlowStartRecord{}, &EventRecord{}); err != nil {
		t.Fatal(err)
	}
	// Reproduce the already-deployed indexes, without adding a production schema
	// change or pretending the two composite event indexes cover global time.
	for _, statement := range []string{
		"CREATE INDEX idx_subscription_flow_start_received ON subscription_flow_start_events(received_at)",
		"CREATE INDEX idx_subscription_fair_use_event_subscription_time ON subscription_fair_use_events(subscription_id, occurred_at)",
		"CREATE INDEX idx_subscription_fair_use_event_type_time ON subscription_fair_use_events(event_type, occurred_at)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	var raw, events []struct{ Detail string }
	cutoff := time.Now().UTC().Add(-metering.ObservationRetentionHorizon)
	if err := db.Raw("EXPLAIN QUERY PLAN SELECT id FROM subscription_flow_start_events WHERE received_at < ? ORDER BY received_at ASC, id ASC LIMIT 1000", cutoff).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("EXPLAIN QUERY PLAN SELECT id FROM subscription_fair_use_events WHERE occurred_at < ? ORDER BY occurred_at ASC, id ASC LIMIT 1000", cutoff).Scan(&events).Error; err != nil {
		t.Fatal(err)
	}
	var rawPlan, eventPlan string
	for _, step := range raw {
		rawPlan += step.Detail + "; "
	}
	for _, step := range events {
		eventPlan += step.Detail + "; "
	}
	t.Logf("SQLite raw discovery: %s", rawPlan)
	t.Logf("SQLite evaluation discovery (unbounded scan work remains): %s", eventPlan)
	if !strings.Contains(rawPlan, "idx_subscription_flow_start_received") || strings.Contains(rawPlan, "TEMP B-TREE") {
		t.Fatalf("existing raw time index not used: %s", rawPlan)
	}
	if !strings.Contains(eventPlan, "SCAN") || !strings.Contains(eventPlan, "TEMP B-TREE") {
		t.Fatalf("event discovery scan limitation changed; review its bound: %s", eventPlan)
	}
}

func retentionFlow(at time.Time, event string) FlowStartRecord {
	return FlowStartRecord{NodeID: 1, EventID: event, ReceivedAt: at, OccurredAt: at, CreatedAt: at}
}
func projectRetentionFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i := 0; i < 20; i++ {
		ready, err := (FlowStatistics{DB: db}).HasPending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !ready {
			return
		}
		if _, err := (FlowStatistics{DB: db}).Drain(t.Context(), 200); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("fixture projection did not finish in bounded batches")
}
