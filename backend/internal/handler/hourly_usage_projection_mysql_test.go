package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestMySQLHourlyProjectionFailureAndRebuildPreservePendingCheckpoint(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	ctx := context.Background()
	if err := h.db.Exec(`CREATE TRIGGER reject_hourly_chart BEFORE INSERT ON traffic_usage_hourly FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='hourly chart unavailable'`).Error; err != nil {
		t.Fatal(err)
	}
	events := accountingBenchmarkEvents(credential, 0, 4)
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal("hourly chart failure blocked committed settlement", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
	if _, err := h.services.UsageSummary().Read(ctx, credential.UserID, metering.UsageSummaryQuery{}); !errors.Is(err, metering.ErrUsageProjectionPending) {
		t.Fatal("pending summary returned complete numbers", err)
	}
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	if count, err := worker.Drain(ctx, 200); err == nil || count != 0 {
		t.Fatal(count, err)
	}
	for _, table := range []string{"traffic_usage_hourly", "protocol_endpoint_usage_daily"} {
		var count int64
		if err := h.db.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if err := h.db.Exec("DROP TRIGGER reject_hourly_chart").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		rebuildFixtureHourly(t, h.db)
	}
	var used int64
	if err := h.db.Table("traffic_usage_hourly").Select("COALESCE(SUM(used_bytes),0)").Scan(&used).Error; err != nil || used != 0 {
		t.Fatal("rebuild pre-applied pending delta", used, err)
	}
	drainUsageProjections(t, h.db)
	if err := h.projectZeroNodeEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	full, err := h.services.UsageSummary().Read(ctx, credential.UserID, metering.UsageSummaryQuery{})
	if err != nil || full.TotalUsedBytes != 120 {
		t.Fatal(full, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
}

func TestMySQLSlowHourlyProjectionDoesNotHoldCoreIntentInsertGap(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	ctx := context.Background()
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 0, 4)); err != nil {
		t.Fatal(err)
	}
	var record model.TrafficRecord
	if err := h.db.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	hour := record.At.UTC().Truncate(time.Hour).Format("2006-01-02 15:04:05")
	bucket := map[string]any{"record_at": hour, "user_id": record.UserID, "subscription_id": record.SubscriptionID, "node_id": record.NodeID, "protocol_endpoint_id": record.ProtocolEndpointID, "protocol_multiplier_milli": record.ProtocolMultiplierMilli, "used_bytes": 0, "record_count": 0}
	if err := h.db.Table("traffic_usage_hourly").Create(bucket).Error; err != nil {
		t.Fatal(err)
	}
	lock := h.db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	defer lock.Rollback()
	var held struct{ Used int64 }
	if err := lock.Table("traffic_usage_hourly").Clauses(clause.Locking{Strength: "UPDATE"}).Select("used_bytes AS used").Find(&held).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	if err := h.db.Callback().Create().Before("gorm:create").Register("test:hourly_upsert_started", func(tx *gorm.DB) {
		if tx.Statement.Table == "traffic_usage_hourly" {
			select {
			case started <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := (meteringstore.UsageProjectionWorker{DB: h.db}).Drain(workerCtx, 200); done <- err }()
	select {
	case <-started:
	case <-workerCtx.Done():
		t.Fatal("worker did not reach the locked hourly bucket")
	}
	// The queue contains fewer than the batch limit. REPEATABLE READ would
	// hold its supremum gap and block every new core intent while hourly waits.
	coreCtx, coreCancel := context.WithTimeout(ctx, 2*time.Second)
	defer coreCancel()
	next := accountingBenchmarkEvents(credential, 1, 4)
	for i := range next {
		next[i].OccurredAt = record.At
	}
	if err := h.projectZeroNodeEvents(coreCtx, next); err != nil {
		t.Fatal("slow chart blocked authoritative intent insertion", err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 240, subStatusActive)
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	full, err := h.services.UsageSummary().Read(ctx, credential.UserID, metering.UsageSummaryQuery{})
	if err != nil || full.TotalUsedBytes != 240 {
		t.Fatal(full, err)
	}
}

func TestMySQLHourlyProjectionSchemaResumesPartialDDLAndRefusesLostCoverage(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 0, 1)); err != nil {
		t.Fatal(err)
	}
	drainUsageProjections(t, h.db)
	if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 1, 1)); err != nil {
		t.Fatal(err)
	}
	const version = "0054_hourly_usage_projection.up.sql"
	if err := h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec("DROP INDEX idx_usage_hourly_pending_user ON metering_usage_projection_pending").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(h.db); err != nil {
			t.Fatal(err)
		}
	}
	var pending model.MeteringUsageProjectionPending
	if err := h.db.First(&pending).Error; err != nil || !pending.HourlyPending || pending.RecordAt == nil {
		t.Fatal("partial DDL reset completion evidence", pending, err)
	}
	// Keep both old and new migration history. A lost derived table, or a
	// crash-created empty replacement, must rebuild only confirmed facts.
	if err := h.db.Migrator().DropTable("traffic_usage_hourly"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(h.db); err != nil {
			t.Fatal(err)
		}
		var used int64
		if err := h.db.Table("traffic_usage_hourly").Select("SUM(used_bytes)").Scan(&used).Error; err != nil || used != 30 {
			t.Fatal("derived recovery pre-applied pending fact", used, err)
		}
		if i == 0 {
			if err := h.db.Exec("DELETE FROM traffic_usage_hourly").Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := h.db.Exec("DROP INDEX idx_usage_hourly_pending_subscription ON metering_usage_projection_pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec("ALTER TABLE metering_usage_projection_pending DROP COLUMN subscription_id").Error; err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(h.db); err == nil {
		t.Fatal("recorded missing scope coverage fabricated")
	}
}

func TestMySQLHourlyProjectionCalendarGuardMatchesHalfQuarterHourEdges(t *testing.T) {
	for _, timezone := range []string{"Asia/Kolkata", "Asia/Kathmandu"} {
		t.Run(timezone, func(t *testing.T) {
			h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
			location, err := time.LoadLocation(timezone)
			if err != nil {
				t.Fatal(err)
			}
			from := time.Date(2026, 10, 19, 0, 0, 0, 0, location)
			q := metering.TrafficTrendQuery{From: from, Days: 2, Timezone: timezone}
			for i := 0; i < 2; i++ {
				day := from.AddDate(0, 0, i)
				q.Buckets = append(q.Buckets, metering.TrendBucket{Key: day.Format("2006-01-02"), StartUTC: day.UTC(), EndUTC: day.AddDate(0, 0, 1).UTC()})
			}
			boundary := q.Buckets[0].EndUTC
			events := accountingBenchmarkEvents(credential, 0, 2)
			events[0].OccurredAt = boundary.Add(-10 * time.Minute)
			events[1].OccurredAt = boundary.Add(10 * time.Minute)
			if err := h.projectZeroNodeEvents(context.Background(), events); err != nil {
				t.Fatal(err)
			}
			out, err := h.services.TrafficTrends(nil).Read(context.Background(), credential.UserID, q)
			if err != nil || out.Snapshot.RecordCount != 2 || out.Snapshot.Points[0].UsedBytes != 30 || out.Snapshot.Points[1].UsedBytes != 30 {
				t.Fatal("calendar raw edges waited on unused pending hourly bucket", out, err)
			}
			next := accountingBenchmarkEvents(credential, 1, 2)
			for i := range next {
				next[i].OccurredAt = q.Buckets[0].StartUTC.Truncate(time.Hour).Add(2 * time.Hour)
			}
			if err := h.projectZeroNodeEvents(context.Background(), next); err != nil {
				t.Fatal(err)
			}
			if _, err := h.services.TrafficTrends(nil).Read(context.Background(), credential.UserID, q); !errors.Is(err, metering.ErrUsageProjectionPending) {
				t.Fatal("calendar accepted an incomplete actual full-hour bucket", err)
			}
			drainUsageProjections(t, h.db)
			out, err = h.services.TrafficTrends(nil).Read(context.Background(), credential.UserID, q)
			if err != nil || out.Snapshot.RecordCount != 4 || out.Snapshot.Points[0].UsedBytes != 90 || out.Snapshot.Points[1].UsedBytes != 30 {
				t.Fatal("calendar source changed after rollup", out, err)
			}
			assertAccountingTotal(t, h, credential.SubscriptionID, 120, subStatusActive)
		})
	}
}
