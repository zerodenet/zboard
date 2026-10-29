package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/meteringstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestProjectionWorkerRefusesUnpreparedWithoutWrites(t *testing.T) {
	db := schemaFixture(t, datastore.DriverSQLite)
	before := preparedProjectionSnapshot(t, db)
	if err := RunProjectionWorker(t.Context(), db, time.Second); !errors.Is(err, datastore.ErrProjectionNotPrepared) {
		t.Fatal("unprepared worker was admitted", err)
	}
	if after := preparedProjectionSnapshot(t, db); after != before {
		t.Fatal("refused worker prepared or changed database")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := RunProjectionWorker(ctx, db, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled worker performed admission", err)
	}
	for _, budget := range []time.Duration{0, time.Millisecond, 5*time.Minute + time.Second} {
		if err := RunProjectionWorker(t.Context(), db, budget); err == nil {
			t.Fatal("invalid drain budget admitted")
		}
	}
	if err := RunProjectionWorker(nil, db, time.Second); err == nil {
		t.Fatal("nil process context admitted")
	}
}

func TestSQLiteProjectionWorkerMaintenanceCompetitionAndDrain(t *testing.T) {
	assertProjectionWorkerCompetition(t, datastore.DriverSQLite)
}
func TestMySQLProjectionWorkerMaintenanceCompetitionAndDrain(t *testing.T) {
	assertProjectionWorkerCompetition(t, datastore.DriverMySQL)
}

func assertProjectionWorkerCompetition(t *testing.T, driver string) {
	db := schemaFixture(t, driver)
	if err := PrepareDatabaseSchema(db); err != nil {
		t.Fatal(err)
	}
	setup := New(db, "worker-test")
	if err := setup.SystemConfigDefaults.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	setup.Close()
	user := model.User{Email: "worker@example.test", Password: "unused", Status: "active"}
	group := model.NodeGroup{Name: "worker", Code: "worker", IsEnabled: true}
	node := model.Node{Name: "worker", Config: "{}"}
	for _, row := range []any{&user, &group, &node} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{Name: "worker", Slug: "worker", NodeGroupID: group.ID, IsActive: true}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sku := model.PlanSKU{PlanID: plan.ID, Code: "worker", Name: "worker", BillingUnit: "month", BillingValue: 1, PriceCents: 100, Currency: "CNY"}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	sub := model.Subscription{UserID: user.ID, PlanID: plan.ID, PlanSKUID: sku.ID, NodeGroupID: group.ID, Status: "active", StartAt: now, EndAt: now.Add(24 * time.Hour), FlowTotal: 10000, FlowUsed: 777, Config: "{}"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, RuntimeKey: uuid.NewString(), Name: "worker", Protocol: "vless", Port: 443, IsActive: true, ServerConfig: "{}", ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	var originalSub model.Subscription
	if err := db.First(&originalSub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	enqueue := func(first, count int) {
		t.Helper()
		if err := db.Transaction(func(tx *gorm.DB) error {
			rows := make([]model.TrafficRecord, count)
			for i := range rows {
				rows[i] = model.TrafficRecord{UserID: user.ID, SubscriptionID: sub.ID, NodeID: node.ID, ProtocolEndpointID: endpoint.ID, ReportID: fmt.Sprintf("worker-report-%d", first+i), Nonce: fmt.Sprintf("worker-nonce-%d", first+i), RawBytes: 3, DownloadBytes: 3, UsedBytes: 5, ProtocolMultiplierMilli: 1500, At: now}
			}
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
			return meteringstore.EnqueueUsageProjections(tx, rows)
		}); err != nil {
			t.Fatal(err)
		}
	}
	enqueue(0, 64)
	store := jobstore.New(db)
	foreign, err := store.Submit(t.Context(), jobs.Submission{Owner: "system", Key: "worker-foreign", Handler: "foreign-worker", Resource: "foreign:worker", Payload: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Claim(t.Context(), "foreign-process", []string{"foreign-worker"}, time.Hour)
	if err != nil || lease.Run.ID != foreign.ID {
		t.Fatal("foreign lease setup", err)
	}
	if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", "maintenance_enabled").Update("value", "true").Error; err != nil {
		t.Fatal(err)
	}
	stopped := make([]bool, 2)
	contexts := make([]context.CancelFunc, 2)
	results := make([]chan error, 2)
	for i := range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		contexts[i] = cancel
		results[i] = make(chan error, 1)
		go func() { results[i] <- RunProjectionWorker(ctx, db, 5*time.Second) }()
	}
	t.Cleanup(func() {
		for _, cancel := range contexts {
			cancel()
		}
		for i := range results {
			if !stopped[i] {
				select {
				case <-results[i]:
				case <-time.After(8 * time.Second):
					t.Errorf("worker %d did not stop during cleanup", i)
				}
			}
		}
	})
	select {
	case err := <-results[0]:
		stopped[0] = true
		t.Fatal("worker stopped instead of remaining paused", err)
	case err := <-results[1]:
		stopped[1] = true
		t.Fatal("worker stopped instead of remaining paused", err)
	case <-time.After(1500 * time.Millisecond):
	}
	var pending int64
	if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil || pending != 64 {
		t.Fatalf("maintenance consumed pending: %d %v", pending, err)
	}
	if err := db.Model(&model.SystemConfig{}).Where("config_key = ?", "maintenance_enabled").Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	waitEmpty := func() {
		t.Helper()
		deadline := time.NewTimer(12 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for {
			if err := db.Model(&model.MeteringUsageProjectionPending{}).Count(&pending).Error; err != nil {
				t.Fatal(err)
			}
			if pending == 0 {
				return
			}
			select {
			case <-deadline.C:
				t.Fatal("workers failed to recover pending projections")
			case <-tick.C:
			}
		}
	}
	waitEmpty()
	stop := func(i int) {
		t.Helper()
		contexts[i]()
		select {
		case err := <-results[i]:
			stopped[i] = true
			if err != nil {
				t.Fatal("worker did not drain", err)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("worker drain did not finish")
		}
	}
	stop(0)
	enqueue(64, 32)
	waitEmpty()
	stop(1)
	var daily struct{ Used, Raw, Records int64 }
	if err := db.Model(&model.ProtocolEndpointUsageDaily{}).Select("COALESCE(SUM(used_bytes),0) AS used, COALESCE(SUM(raw_bytes),0) AS raw, COALESCE(SUM(record_count),0) AS records").Scan(&daily).Error; err != nil || daily.Used != 480 || daily.Raw != 288 || daily.Records != 96 {
		t.Fatalf("daily applied twice or lost: %+v %v", daily, err)
	}
	var hourly struct{ Used, Raw, Records int64 }
	if err := db.Table("traffic_usage_hourly").Select("COALESCE(SUM(used_bytes),0) AS used, COALESCE(SUM(raw_bytes),0) AS raw, COALESCE(SUM(record_count),0) AS records").Scan(&hourly).Error; err != nil || hourly.Used != 480 || hourly.Raw != 288 || hourly.Records != 96 {
		t.Fatalf("hourly applied twice or lost: %+v %v", hourly, err)
	}
	var actualSub model.Subscription
	if err := db.First(&actualSub, sub.ID).Error; err != nil || !reflect.DeepEqual(actualSub, originalSub) {
		t.Fatalf("auxiliary worker changed entitlement: %+v %v", actualSub, err)
	}
	var actualForeign jobstore.Record
	if err := db.First(&actualForeign, "id = ?", foreign.ID).Error; err != nil || actualForeign.Token != lease.Token || actualForeign.Worker != "foreign-process" || actualForeign.State != string(jobs.Running) {
		t.Fatalf("local drain canceled foreign lease: %+v %v", actualForeign, err)
	}
	if err := db.Model(&model.TrafficRecord{}).Count(&pending).Error; err != nil || pending != 96 {
		t.Fatalf("worker changed fact count: %d %v", pending, err)
	}
}
