package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestMySQLUsageProjectionCompetingWorkersAndCheckpointRecovery(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	ctx := context.Background()
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 0, 8)); err != nil {
		t.Fatal(err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 240, subStatusActive)
	usage, err := (networkstore.Inventory{DB: h.db}).ProtocolUsage(ctx, []uint{credential.ProtocolEndpointID}, time.Now().UTC())
	if err != nil || !usage[credential.ProtocolEndpointID].TrafficProjectionPending {
		t.Fatalf("pending inventory projection: %+v %v", usage, err)
	}
	if err := h.db.Exec(`CREATE TRIGGER reject_usage_checkpoint BEFORE DELETE ON metering_usage_projection_pending FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='checkpoint unavailable'`).Error; err != nil {
		t.Fatal(err)
	}
	worker := meteringstore.UsageProjectionWorker{DB: h.db}
	if count, err := worker.Drain(ctx, 4); err == nil || count != 0 {
		t.Fatalf("checkpoint failure acknowledged: %d %v", count, err)
	}
	var pending, daily int64
	if err := h.db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil || pending != 8 {
		t.Fatal(pending, err)
	}
	if err := h.db.Model(&model.ProtocolEndpointUsageDaily{}).Count(&daily).Error; err != nil || daily != 0 {
		t.Fatal(daily, err)
	}
	if err := h.db.Exec("DROP TRIGGER reject_usage_checkpoint").Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type outcome struct {
		count int
		err   error
	}
	done := make(chan outcome, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start
			count, err := worker.Drain(ctx, 4)
			done <- outcome{count, err}
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < 2; i++ {
		result := <-done
		if result.err != nil {
			t.Fatalf("competing projection worker: count=%d err=%v", result.count, result.err)
		}
	}
	drainUsageProjections(t, h.db)
	var projection model.ProtocolEndpointUsageDaily
	if err := h.db.First(&projection, "protocol_endpoint_id = ?", credential.ProtocolEndpointID).Error; err != nil || projection.UsedBytes != 240 || projection.RecordCount != 8 {
		t.Fatal(projection, err)
	}
	assertAccountingTotal(t, h, credential.SubscriptionID, 240, subStatusActive)
	usage, err = (networkstore.Inventory{DB: h.db}).ProtocolUsage(ctx, []uint{credential.ProtocolEndpointID}, time.Now().UTC())
	if err != nil || usage[credential.ProtocolEndpointID].TrafficProjectionPending || usage[credential.ProtocolEndpointID].UsedBytesTotal != 240 {
		t.Fatalf("recovered inventory projection: %+v %v", usage, err)
	}
	if err := h.projectZeroNodeEvents(ctx, accountingBenchmarkEvents(credential, 0, 8)); err != nil {
		t.Fatal(err)
	}
	if count, err := worker.Drain(ctx, 4); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestMySQLUsageProjectionSchemaPreservesInterruptedIntentsAndRefusesLostQueue(t *testing.T) {
	h, credential := accountingFixtureFromOrder(t, newMySQLOrderFixture(t))
	if err := h.projectZeroNodeEvents(context.Background(), accountingBenchmarkEvents(credential, 0, 1)); err != nil {
		t.Fatal(err)
	}
	const version = "0053_metering_usage_projection_pending.up.sql"
	if err := h.db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec("DROP INDEX idx_usage_projection_endpoint ON metering_usage_projection_pending").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := datastore.RunMigrations(h.db); err != nil {
			t.Fatal(err)
		}
	}
	var pending int64
	if err := h.db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil || pending != 1 {
		t.Fatal("schema recovery lost reliable intent", pending, err)
	}
	if err := h.db.Exec("DELETE FROM traffic_records").Error; err == nil {
		t.Fatal("pending source protection missing")
	}
	if err := h.db.Migrator().DropTable("metering_usage_projection_pending"); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(h.db); err == nil {
		t.Fatal("missing reliable queue silently recreated")
	}
	if h.db.Migrator().HasTable("metering_usage_projection_pending") {
		t.Fatal("failed startup mutated lost-queue evidence")
	}
}
