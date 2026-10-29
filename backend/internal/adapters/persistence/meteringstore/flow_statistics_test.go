package meteringstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func flowStatisticsFixtureSource(t *testing.T, db *gorm.DB, node, sub uint, event string, occurred, received time.Time, enqueue bool) FlowStartRecord {
	t.Helper()
	r := FlowStartRecord{NodeID: node, SubscriptionID: sub, EventID: event, CoreInstanceID: "original-core", OccurredAt: occurred, ReceivedAt: received, CreatedAt: received}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		if enqueue {
			return enqueueFlowStatistics(tx, node, event)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}
func drainFlowStatistics(t *testing.T, db *gorm.DB, limit int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		ready, err := (FlowStatistics{DB: db}).HasPending(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !ready {
			return
		}
		if _, err := (FlowStatistics{DB: db}).Drain(t.Context(), limit); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("bounded flow statistics drain did not finish")
}
func TestFlowStatisticsRealCollectionPendingIsAtomicAndDuplicateDoesNotProjectTwice(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Date(2026, 10, 1, 1, 2, 3, 0, time.UTC)
	collection := metering.FlowCollection{Repository: FlowCollection{DB: db}}
	fail := errors.New("pending write failed")
	if err := db.Callback().Create().Before("gorm:create").Register("reject_flow_pending", func(tx *gorm.DB) {
		if tx.Statement.Table == "flow_start_statistics_pending" {
			tx.AddError(fail)
		}
	}); err != nil {
		t.Fatal(err)
	}
	event := metering.FlowStart{NodeID: 3, EventID: "actual-source", CoreInstanceID: "core", ReceivedAt: at, OccurredAt: at.Add(-time.Hour)}
	if err := collection.Record(t.Context(), event); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove("reject_flow_pending"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"subscription_flow_start_events", "flow_start_statistics_pending", "flow_start_statistics_points"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("source without durable intent committed", table, n, err)
		}
	}
	if err := collection.Record(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if _, _, err := flowStatisticsWindow(db, 0, "received", at.Add(-time.Minute), at); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("pending rendered zero", err)
	}
	drainFlowStatistics(t, db, 200)
	for i := 0; i < 2; i++ {
		if err := collection.Record(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	count, nodes, err := flowStatisticsWindow(db, 0, "received", at.Add(-time.Minute), at)
	if err != nil || count != 1 || nodes != 1 {
		t.Fatal(count, nodes, err)
	}
	count, _, err = flowStatisticsWindow(db, 0, "occurred", at.Add(-time.Minute), at)
	if err != nil || count != 0 {
		t.Fatal("receive/event clocks collapsed", count, err)
	}
}
func TestFlowStatisticsExactInclusiveBoundariesDistinctNodesAndAllOriginalRanges(t *testing.T) {
	db := trafficAggregateDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 17, 123456789, time.UTC)
	// Both close edges, exact minute endpoints and a shared node across multiple
	// buckets expose rounding, double counting and summing distinct-node totals.
	for i, offset := range []time.Duration{-15*24*time.Hour - time.Nanosecond, -15 * 24 * time.Hour, -7 * 24 * time.Hour, -3 * 24 * time.Hour, -24 * time.Hour, -24*time.Hour + time.Nanosecond, -time.Minute, -17*time.Second - 123456789*time.Nanosecond, -time.Nanosecond, 0, time.Nanosecond} {
		node := uint(i%2 + 1)
		received := now.Add(offset)
		flowStatisticsFixtureSource(t, db, node, 7, fmt.Sprint("edge-", i), received.Add(-2*time.Hour), received, true)
	}
	drainFlowStatistics(t, db, 2)
	for _, name := range []string{"1d", "3d", "7d", "15d"} {
		spec, err := metering.ParseObservationRange(name)
		if err != nil {
			t.Fatal(err)
		}
		rows, nodes, err := flowStatisticsSeries(db, 7, spec, now)
		if err != nil {
			t.Fatal(err)
		}
		seconds := int64(spec.BucketDuration / time.Second)
		var expected []metering.ObservationAggregate
		if err := db.Model(&FlowStartRecord{}).Select("CAST(CAST(strftime('%s',received_at) AS INTEGER)/? AS INTEGER) AS bucket_index,COUNT(*) AS connection_starts,COUNT(DISTINCT node_id) AS working_nodes", seconds).Where("subscription_id=? AND received_at>=? AND received_at<=?", 7, now.Add(-spec.Duration), now).Group("bucket_index").Order("bucket_index").Scan(&expected).Error; err != nil {
			t.Fatal(err)
		}
		var exactNodes int64
		if err := db.Model(&FlowStartRecord{}).Where("subscription_id=? AND received_at>=? AND received_at<=?", 7, now.Add(-spec.Duration), now).Distinct("node_id").Count(&exactNodes).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rows, expected) || nodes != exactNodes {
			t.Fatal(name, rows, expected, nodes, exactNodes)
		}
	}
	// A to endpoint exactly on a minute must include its exact point once.
	end := now.Truncate(time.Minute)
	from := end.Add(-24 * time.Hour)
	for _, basis := range []string{"received", "occurred"} {
		count, nodes, err := flowStatisticsWindow(db, 7, basis, from, end)
		if err != nil {
			t.Fatal(err)
		}
		column, _ := flowStatisticsColumn(basis)
		var exact struct{ Count, Nodes int64 }
		if err := db.Model(&FlowStartRecord{}).Select("COUNT(*) AS count,COUNT(DISTINCT node_id) AS nodes").Where("subscription_id=? AND "+column+">=? AND "+column+"<=?", 7, from, end).Scan(&exact).Error; err != nil {
			t.Fatal(err)
		}
		if count != exact.Count || nodes != exact.Nodes {
			t.Fatal(basis, count, nodes, exact)
		}
	}
}
func TestFlowStatisticsLegacyResumeRetireAndCopiedAllocatorReuseStayExplicit(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		flowStatisticsFixtureSource(t, db, 1, 7, fmt.Sprint("legacy-", i), at, at.Add(time.Duration(i)*time.Second), false)
	}
	// This isolated fixture reconstructs a genuine pre0071 source schema.
	if err := db.Exec("DELETE FROM schema_migrations WHERE version=?", "0071_flow_start_statistics.up.sql").Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"flow_start_statistics_state", "flow_start_statistics_pending", "flow_start_statistics_points", "flow_start_statistics_minutes"} {
		if err := db.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropIndex("subscription_flow_start_events", "idx_flow_start_statistics_registered"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn("subscription_flow_start_events", "statistics_registered"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	if n, err := (FlowStatistics{DB: db}).Drain(t.Context(), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var first model.FlowStartStatisticsState
	if err := db.First(&first, 1).Error; err != nil || first.LegacyProjectedRows != 1 {
		t.Fatal(first, err)
	}
	fail := errors.New("interrupted backfill")
	if err := db.Callback().Create().Before("gorm:create").Register("fail_flow_point", func(tx *gorm.DB) {
		if tx.Statement.Table == "flow_start_statistics_points" {
			tx.AddError(fail)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (FlowStatistics{DB: db}).Drain(t.Context(), 1); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove("fail_flow_point"); err != nil {
		t.Fatal(err)
	}
	var held model.FlowStartStatisticsState
	if err := db.First(&held, 1).Error; err != nil || !reflect.DeepEqual(first, held) {
		t.Fatal("failed backfill advanced coverage", first, held, err)
	}
	drainFlowStatistics(t, db, 1)
	if n, err := (ObservationRetention{DB: db}).PruneFlowStarts(t.Context(), at.Add(3*time.Second), 1000); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	for _, table := range []string{"subscription_flow_start_events", "flow_start_statistics_points", "flow_start_statistics_minutes"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("finite retention left historical dedup or minute contribution", table, n, err)
		}
	}
	target := trafficAggregateDB(t)
	if err := datastore.WithCopyDestination(t.Context(), target, func(tx *gorm.DB) error {
		tables, err := datastore.MigrationTables(tx)
		if err != nil {
			return err
		}
		if err := datastore.ClearCopyDestinationRows(tx, tables); err != nil {
			return err
		}
		return datastore.CopyApplicationData(db, tx)
	}); err != nil {
		t.Fatal(err)
	}
	fresh := flowStatisticsFixtureSource(t, target, 1, 7, "legacy-0", at.AddDate(0, 0, 16), at.AddDate(0, 0, 16), true)
	if fresh.ID >= first.LegacyUpperID {
		t.Fatal("fixture did not actually exercise copied allocator reset", fresh.ID, first.LegacyUpperID)
	}
	drainFlowStatistics(t, target, 200)
	// An old writer after allocator reset must be detected even below old MAX.
	unregistered := flowStatisticsFixtureSource(t, target, 2, 7, "old-writer", fresh.OccurredAt, fresh.ReceivedAt, false)
	if unregistered.ID >= 3 {
		t.Fatal("unregistered source is not below old head", unregistered.ID)
	}
	if _, _, err := flowStatisticsWindow(target, 7, "received", fresh.ReceivedAt.Add(-time.Minute), fresh.ReceivedAt); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("small reused ID bypassed pending coverage", err)
	}
	drainFlowStatistics(t, target, 200)
	count, nodes, err := flowStatisticsWindow(target, 7, "received", fresh.ReceivedAt.Add(-time.Minute), fresh.ReceivedAt)
	if err != nil || count != 2 || nodes != 2 {
		t.Fatal(count, nodes, err)
	}
}
func TestFlowStatisticsRetirementHoldsCorruptionAndPreservesFiniteReplayTTL(t *testing.T) {
	db := trafficAggregateDB(t)
	at := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC)
	bad := flowStatisticsFixtureSource(t, db, 1, 7, "bad", at, at, true)
	good := flowStatisticsFixtureSource(t, db, 2, 7, "good", at, at, true)
	drainFlowStatistics(t, db, 200)
	if err := db.Model(&model.FlowStartStatisticsPoint{}).Where("source_id=?", bad.ID).Update("fact_sha256", "corrupt").Error; err != nil {
		t.Fatal(err)
	}
	n, err := (ObservationRetention{DB: db}).PruneFlowStarts(context.Background(), at.Add(time.Second), 1000)
	if err == nil || n != 1 {
		t.Fatal("corrupt candidate hid healthy progress", n, err)
	}
	if err := db.First(&FlowStartRecord{}, bad.ID).Error; err != nil {
		t.Fatal("corrupt original lost", err)
	}
	if err := db.First(&FlowStartRecord{}, good.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("healthy old source not retired", err)
	}
	// Same source event may be newly observed after the original diagnostic TTL.
	later := at.AddDate(0, 0, 16)
	replay := flowStatisticsFixtureSource(t, db, 2, 7, "good", at, later, true)
	if replay.ID == good.ID {
		t.Fatal("old diagnostic identity retained forever")
	}
	drainFlowStatistics(t, db, 200)
	count, nodes, err := flowStatisticsWindow(db, 7, "received", later.Add(-time.Minute), later)
	if err != nil || count != 1 || nodes != 1 {
		t.Fatal(count, nodes, err)
	}
}
